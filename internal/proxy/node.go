package proxy

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// NodeConfig represents one Crafty Controller instance (node).
type NodeConfig struct {
	ID            string `yaml:"id" json:"id"`
	Name          string `yaml:"name" json:"name"`
	Type          string `yaml:"type" json:"type"` // lxc, vm, baremetal
	IP            string `yaml:"ip" json:"ip"`
	CraftyPort    int    `yaml:"crafty_port" json:"crafty_port"`
	CraftyToken   string `yaml:"crafty_token" json:"-"`
	// Optional — only if the node can be woken up.
	ProxmoxHost   string `yaml:"proxmox_host,omitempty" json:"proxmox_host,omitempty"`
	ProxmoxNode   string `yaml:"proxmox_node,omitempty" json:"proxmox_node,omitempty"`
	ProxmoxVMID   string `yaml:"proxmox_vmid,omitempty" json:"proxmox_vmid,omitempty"`
	WOLMAC        string `yaml:"wol_mac,omitempty" json:"wol_mac,omitempty"`
	WOLBroadcast  string `yaml:"wol_broadcast,omitempty" json:"wol_broadcast,omitempty"`
}

// NodesConfig is the root of nodes.yml.
type NodesConfig struct {
	Nodes []NodeConfig `yaml:"nodes" json:"nodes"`
}

// LoadNodes reads nodes.yml from path. If missing, returns nil.
func LoadNodes(path string) (*NodesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("nodes config: %w", err)
	}
	var nc NodesConfig
	if err := yaml.Unmarshal(data, &nc); err != nil {
		return nil, fmt.Errorf("nodes config: %w", err)
	}
	if len(nc.Nodes) == 0 {
		return nil, fmt.Errorf("nodes config: %s has no nodes defined", path)
	}
	for i, n := range nc.Nodes {
		if n.ID == "" {
			return nil, fmt.Errorf("nodes config: node[%d] missing id", i)
		}
		if n.IP == "" {
			return nil, fmt.Errorf("nodes config: node[%d] (%s) missing ip", i, n.ID)
		}
		if n.CraftyPort == 0 {
			n.CraftyPort = 8443
		}
		nc.Nodes[i] = n
	}
	return &nc, nil
}

// LookupNode finds a node by ID.
func (nc *NodesConfig) LookupNode(id string) *NodeConfig {
	for i := range nc.Nodes {
		if nc.Nodes[i].ID == id {
			return &nc.Nodes[i]
		}
	}
	return nil
}

// MigrateFromEnv creates a nodes.yml with a single "default" node from env vars.
func MigrateFromEnv(path string, cfg *Config) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	node := NodeConfig{
		ID:           "default",
		Name:         "Default Node",
		Type:         "lxc",
		IP:           cfg.CraftyHost,
		CraftyPort:   atoi(cfg.CraftyPort),
		ProxmoxHost:  cfg.ProxmoxHost,
		ProxmoxNode:  cfg.ProxmoxNode,
		ProxmoxVMID:  cfg.ProxmoxLXCID,
		WOLMAC:       cfg.WOLMAC,
		WOLBroadcast: cfg.WOLBroadcast,
	}
	nc := &NodesConfig{Nodes: []NodeConfig{node}}
	data, err := yaml.Marshal(nc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	// Migrate servers.yml: add node_id to existing servers.
	if cfg.Servers != nil {
		for i := range cfg.Servers.Servers {
			cfg.Servers.Servers[i].NodeID = "default"
		}
		data2, _ := yaml.Marshal(cfg.Servers)
		os.WriteFile(cfg.ServersPath, data2, 0644)
	}
	return nil
}

func atoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}
