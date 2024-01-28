package wireguard

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/USA-RedDragon/aredn-manager/internal/db/models"
	"golang.org/x/sync/errgroup"
	"golang.zx2c4.com/wireguard/wgctrl"
	"gorm.io/gorm"
)

const defTimeout = 10 * time.Second

type Manager struct {
	db                    *gorm.DB
	peerAddChan           chan models.Tunnel
	peerAddConfirmChan    chan models.Tunnel
	peerRemoveChan        chan models.Tunnel
	peerRemoveConfirmChan chan models.Tunnel
	shutdownChan          chan struct{}
	shutdownConfirmChan   chan struct{}
	activePeers           sync.Map
	wgClient              *wgctrl.Client
}

func NewManager(db *gorm.DB) (*Manager, error) {
	wgClient, err := wgctrl.New()
	if err != nil {
		return nil, err
	}
	return &Manager{
		db:                    db,
		peerAddChan:           make(chan models.Tunnel),
		peerAddConfirmChan:    make(chan models.Tunnel),
		peerRemoveChan:        make(chan models.Tunnel),
		peerRemoveConfirmChan: make(chan models.Tunnel),
		shutdownChan:          make(chan struct{}),
		shutdownConfirmChan:   make(chan struct{}),
		activePeers:           sync.Map{},
		wgClient:              wgClient,
	}, nil
}

func (m *Manager) Run() error {
	go m.run()
	return m.initializeTunnels()
}

func (m *Manager) removeAllPeers() error {
	errGroup := &errgroup.Group{}
	m.activePeers.Range(func(key, value interface{}) bool {
		peer, ok := value.(models.Tunnel)
		if !ok {
			return true
		}
		errGroup.Go(func() error {
			return m.RemovePeer(peer)
		})
		return true
	})

	return errGroup.Wait()
}

func (m *Manager) Stop() error {
	// Remove all peers, then stop the thread and close the channels
	err := m.removeAllPeers()
	if err != nil {
		return err
	}
	m.shutdownChan <- struct{}{}
	<-m.shutdownConfirmChan
	return nil
}

func (m *Manager) initializeTunnels() error {
	tunnels, err := models.ListWireguardTunnels(m.db)
	if err != nil {
		return err
	}
	errGroup := &errgroup.Group{}
	for _, tunnel := range tunnels {
		tunnel := tunnel
		errGroup.Go(func() error {
			return m.AddPeer(tunnel)
		})
	}

	return errGroup.Wait()
}

func (m *Manager) run() {
	for {
		select {
		case peer := <-m.peerAddChan:
			go m.addPeer(peer)
		case peer := <-m.peerRemoveChan:
			go m.removePeer(peer)
		case <-m.shutdownChan:
			close(m.peerAddChan)
			close(m.peerRemoveChan)
			close(m.peerAddConfirmChan)
			close(m.peerRemoveConfirmChan)
			m.shutdownConfirmChan <- struct{}{}
			return
		}
	}
}

func (m *Manager) addPeer(peer models.Tunnel) {
	// Create a new wireguard interface listening on the port from the peer tunnel
	// If the peer is a client, then the password is the public key of the client
	// If the peer is a server, then the password is the private key of the server
	// TODO: add peer
	log.Println("adding peer", peer)

	// if peer.WireguardServerKey != "" {
	// 	m.wgClient.ConfigureDevice(fmt.Sprintf("wgs%d", serverNum), wgtypes.Config{})
	// } else {
	// 	m.wgClient.ConfigureDevice(fmt.Sprintf("wgc%d", clientNum), wgtypes.Config{})
	// }

	m.activePeers.Store(peer.ID, peer)

	m.peerAddConfirmChan <- peer
}

func (m *Manager) removePeer(peer models.Tunnel) {
	// TODO: remove peer
	log.Println("removing peer", peer)

	m.activePeers.Delete(peer.ID)

	m.peerRemoveConfirmChan <- peer
}

func (m *Manager) AddPeer(peer models.Tunnel) error {
	m.peerAddChan <- peer

	ctx, cancel := context.WithTimeout(context.Background(), defTimeout)
	defer cancel()

	return m.waitForPeerAddition(ctx, peer)
}

func (m *Manager) waitForPeerAddition(ctx context.Context, peer models.Tunnel) error {
	select {
	case <-m.shutdownChan:
		return fmt.Errorf("wireguard manager is shutting down")
	case <-ctx.Done():
		return ctx.Err()
	case addedPeer := <-m.peerAddConfirmChan:
		if addedPeer.ID != peer.ID {
			return m.waitForPeerAddition(ctx, peer)
		}
		return nil
	}
}

func (m *Manager) RemovePeer(peer models.Tunnel) error {
	m.peerRemoveChan <- peer

	ctx, cancel := context.WithTimeout(context.Background(), defTimeout)
	defer cancel()

	return m.waitForPeerRemoval(ctx, peer)
}

func (m *Manager) waitForPeerRemoval(ctx context.Context, peer models.Tunnel) error {
	select {
	case <-m.shutdownChan:
		return fmt.Errorf("wireguard manager is shutting down")
	case <-ctx.Done():
		return ctx.Err()
	case addedPeer := <-m.peerRemoveConfirmChan:
		if addedPeer.ID != peer.ID {
			return m.waitForPeerRemoval(ctx, peer)
		}
		return nil
	}
}
