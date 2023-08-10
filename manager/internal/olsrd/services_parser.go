package olsrd

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

const SERVICES_FILE = "/var/run/services_olsr"

type AREDNService struct {
	URL        *url.URL
	Protocol   string
	Name       string
	ShouldLink bool
}

func (s *AREDNService) String() string {
	ret := ""
	ret += fmt.Sprintf("%s:\n\t", s.Name)
	if s.ShouldLink {
		ret += fmt.Sprintf("%s\t%s", s.Protocol, s.URL.String())
	} else {
		ret += fmt.Sprintf("%s\t%s", s.Protocol, s.URL.Hostname())
	}
	return ret
}

type ServicesParser struct {
	currentServices []*AREDNService
}

func NewServicesParser() *ServicesParser {
	return &ServicesParser{}
}

func (p *ServicesParser) Parse() (err error) {
	services, err := parseServices()
	if err != nil {
		return
	}
	p.currentServices = services
	return
}

func (p *ServicesParser) GetServices() []*AREDNService {
	return p.currentServices
}

func parseServices() (ret []*AREDNService, err error) {
	servicesFile, err := os.ReadFile(SERVICES_FILE)
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(servicesFile), "\n") {
		// Ignore comments
		if strings.HasPrefix(line, "#") {
			continue
		}

		if strings.Contains(line, "#") {
			line = strings.Split(line, "#")[0]
		}

		line = strings.TrimSpace(line)

		// Ignore empty lines
		if len(line) == 0 {
			continue
		}

		fmt.Printf("Line: %s\n", line)

		// Lines are of the form:
		// url|protocol|name

		// Split the line by '|'
		split := strings.Split(line, "|")
		if len(split) != 3 {
			fmt.Printf("Invalid line in services file: %s\n", line)
			continue
		}

		url, err := url.Parse(split[0])
		if err != nil {
			fmt.Printf("Invalid URL in services file: %s\n", split[0])
			continue
		}

		service := &AREDNService{
			URL:        url,
			Protocol:   split[1],
			Name:       split[2],
			ShouldLink: url.Port() != "0",
		}

		// Add the hostname and IP to the map
		ret = append(ret, service)
	}

	return
}
