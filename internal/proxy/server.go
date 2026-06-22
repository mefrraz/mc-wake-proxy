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

// DiscoveredServer is a server found via Crafty API for the Discover feature.
type DiscoveredServer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
	Players int    `json:"players,omitempty"`
	Version string `json:"version,omitempty"`
	Icon    string `json:"icon,omitempty"`
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

// AddServerToFile adds a server entry to servers.yml, creating the file if needed.
func AddServerToFile(path string, entry ServerEntry) error {
	sc, err := LoadServers(path)
	if err != nil && !os.IsNotExist(err) {
		// If the file exists but is invalid, check the underlying error.
		if !os.IsNotExist(err) {
			return err
		}
	}
	if sc == nil {
		sc = &ServersConfig{}
	}

	// Check for duplicate hostname.
	for _, s := range sc.Servers {
		if equalFoldASCII(s.Hostname, entry.Hostname) {
			return fmt.Errorf("server %q already exists", entry.Hostname)
		}
	}

	sc.Servers = append(sc.Servers, entry)
	return writeServersFile(path, sc)
}

// RemoveServerFromFile removes a server entry by hostname from servers.yml.
func RemoveServerFromFile(path, hostname string) error {
	sc, err := LoadServers(path)
	if err != nil {
		return err
	}
	if sc == nil {
		return fmt.Errorf("no servers configured")
	}

	found := false
	var filtered []ServerEntry
	for _, s := range sc.Servers {
		if equalFoldASCII(s.Hostname, hostname) {
			found = true
			continue
		}
		filtered = append(filtered, s)
	}
	if !found {
		return fmt.Errorf("server %q not found", hostname)
	}

	sc.Servers = filtered
	return writeServersFile(path, sc)
}

func writeServersFile(path string, sc *ServersConfig) error {
	data, err := yaml.Marshal(sc)
	if err != nil {
		return fmt.Errorf("marshal servers YAML: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
