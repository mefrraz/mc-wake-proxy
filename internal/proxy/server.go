package proxy

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ServerEntry represents one Minecraft server backend and its routing hostname.
type ServerEntry struct {
	Hostname        string `yaml:"hostname" json:"hostname"`
	Backend         string `yaml:"backend" json:"backend"`                   // IP:port
	CraftyServerID  string `yaml:"crafty_server_id" json:"crafty_server_id"` // Crafty UUID
}

// ServersConfig is the root of servers.yml.
type ServersConfig struct {
	Servers []ServerEntry `yaml:"servers" json:"servers"`
}

// LoadServers reads servers.yml from path. If the file does not exist, returns
// nil (no error) — the caller should fall back to single-server env config.
func LoadServers(path string) (*ServersConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no file = single-server mode
		}
		return nil, fmt.Errorf("servers config: read %s: %w", path, err)
	}

	var sc ServersConfig
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return nil, fmt.Errorf("servers config: parse %s: %w", path, err)
	}

	if len(sc.Servers) == 0 {
		return nil, fmt.Errorf("servers config: %s has no servers defined", path)
	}

	// Validate each entry.
	for i, s := range sc.Servers {
		if s.Hostname == "" {
			return nil, fmt.Errorf("servers config: server[%d] missing hostname", i)
		}
		if s.Backend == "" {
			return nil, fmt.Errorf("servers config: server[%d] (%s) missing backend", i, s.Hostname)
		}
		if s.CraftyServerID == "" {
			return nil, fmt.Errorf("servers config: server[%d] (%s) missing crafty_server_id", i, s.Hostname)
		}
	}

	return &sc, nil
}

// Lookup finds a server entry by hostname (case-insensitive). Returns nil if not found.
func (sc *ServersConfig) Lookup(hostname string) *ServerEntry {
	for i := range sc.Servers {
		if equalFoldASCII(sc.Servers[i].Hostname, hostname) {
			return &sc.Servers[i]
		}
	}
	return nil
}

// equalFoldASCII is a simple ASCII case-insensitive comparison.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
