package vtun

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/USA-RedDragon/aredn-manager/internal/config"
	"github.com/USA-RedDragon/aredn-manager/internal/db/models"
	"github.com/USA-RedDragon/aredn-manager/internal/utils"
	"gorm.io/gorm"
)

const (
	snippetVtunClientConf = `# This file is generated by the AREDN Manager.
# Do not edit this file directly.
options {
    timeout 60;
    syslog daemon;
    ip /sbin/ip;
    firewall /sbin/iptables;
}`

	snippetVtunClientConfStandardTunnel = `${NAME}-${DASHED_NET} {
    passwd ${PWD};
    device tun${TUN};
    persist yes;
    up {
        ip "addr add ${IP_PLUS_1} peer ${IP_PLUS_2} dev %%";
        ip "link set dev %% up";
        ip "route add ${NET}/30 via ${IP_PLUS_1} mtu 1450";
        firewall "-A FORWARD -i %% -o eth0 -d 10.0.0.0/8 -j ACCEPT";
        firewall "-A FORWARD -i %% -o eth0 -j REJECT";
        firewall "-A FORWARD -i eth0 -o %% -s 10.0.0.0/8 -j ACCEPT";
        firewall "-A FORWARD -i eth0 -o %% -j REJECT";
        ${EXTRA_UP_RULES}
    };
    down {
        ${EXTRA_DOWN_RULES}
        firewall "-D FORWARD -i %% -o eth0 -d 10.0.0.0/8 -j ACCEPT";
        firewall "-D FORWARD -i eth0 -o %% -s 10.0.0.0/8 -j ACCEPT";
        firewall "-D FORWARD -i %% -o eth0 -j REJECT";
        firewall "-D FORWARD -i eth0 -o %% -j REJECT";
        ip "route del ${NET}/30 via ${IP_PLUS_1}";
        ip "link set dev %% down";
        ip "addr del ${IP_PLUS_2} dev %%";
    };
}`

	snippetVtunClientConfWireguardUpRules = `firewall "-A FORWARD -i wg0 -o %% -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT";
        firewall "-A FORWARD -i %% -o wg0 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT";
        firewall "-A FORWARD -i wg0 -o %% -j ACCEPT";
        firewall "-A FORWARD -i %% -o wg0 -j ACCEPT";
        ip "route add ${WG_TAP_PLUS_1}/32 dev wg0";`

	snippetVtunClientConfWireguardDownRules = `firewall "-D FORWARD -i wg0 -o %% -j ACCEPT";
        firewall "-D FORWARD -i %% -o wg0 -j ACCEPT";
        firewall "-D FORWARD -i wg0 -o %% -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT";
        firewall "-D FORWARD -i %% -o wg0 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT";`
)

type tunnelInfo struct {
	Tunnel models.Tunnel
	Tun    int
}

func GenerateAndSaveClient(config *config.Config, db *gorm.DB) error {
	tunnels, err := models.ListClientTunnels(db)
	if err != nil {
		return err
	}

	tunnelMapping := make(map[string]tunnelInfo, 0)

	tun := 100
	for _, tunnel := range tunnels {
		dashedNet := strings.ReplaceAll(tunnel.IP, ".", "-")
		hn := strings.ReplaceAll(tunnel.Hostname, ":", "-") + "-" + dashedNet
		if tunnelMapping[hn] == (tunnelInfo{}) {
			tunnelMapping[hn] = tunnelInfo{
				Tunnel: tunnel,
				Tun:    tun,
			}
		}
		tun++
	}

	for hostname, tunnel_info := range tunnelMapping {
		conf := generateClient(config, tunnel_info)
		if conf == "" {
			return fmt.Errorf("failed to generate vtun-%s.conf", hostname)
		}
		//nolint:golint,gosec
		err = os.WriteFile(
			fmt.Sprintf("/etc/vtund-%s.conf", hostname),
			[]byte(conf),
			0644,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// This file will generate the vtun.conf file
func generateClient(config *config.Config, tunInfo tunnelInfo) string {
	ret := snippetVtunClientConf

	tun := tunInfo.Tun
	tunnel := tunInfo.Tunnel
	ret += "\n\n"
	// We need to replace shell variables in the template with the actual values
	cpSnippetVtunClientConfTunnel := snippetVtunClientConfStandardTunnel
	ip := net.ParseIP(tunnel.IP).To4()
	ipPlus1 := net.IPv4(ip[0], ip[1], ip[2], ip[3]+1)
	ipPlus2 := net.IPv4(ip[0], ip[1], ip[2], ip[3]+2)
	extraUpRules := ""
	extraDownRules := ""
	if config.WireguardTapAddress != "" {
		wgTapIP := net.ParseIP(config.WireguardTapAddress).To4()
		wgTapIPPlus1 := net.IPv4(wgTapIP[0], wgTapIP[1], wgTapIP[2], wgTapIP[3]+1)
		if extraUpRules != "" {
			extraUpRules += "\n        "
			extraDownRules += "\n        "
		}
		extraUpRules += snippetVtunClientConfWireguardUpRules
		extraDownRules += snippetVtunClientConfWireguardDownRules
		utils.ShellReplace(&extraUpRules, map[string]string{
			"WG_TAP_PLUS_1": wgTapIPPlus1.String(),
		})
		utils.ShellReplace(&extraDownRules, map[string]string{
			"WG_TAP_PLUS_1": wgTapIPPlus1.String(),
		})
	}
	utils.ShellReplace(
		&cpSnippetVtunClientConfTunnel,
		map[string]string{
			"NAME":             config.ServerName,
			"DASHED_NET":       strings.ReplaceAll(tunnel.IP, ".", "-"),
			"PWD":              strings.TrimSpace(tunnel.Password),
			"TUN":              fmt.Sprintf("%d", tun),
			"IP_PLUS_1":        ipPlus1.String(),
			"IP_PLUS_2":        ipPlus2.String(),
			"NET":              tunnel.IP,
			"EXTRA_UP_RULES":   extraUpRules,
			"EXTRA_DOWN_RULES": extraDownRules,
		},
	)
	ret += cpSnippetVtunClientConfTunnel

	return ret
}
