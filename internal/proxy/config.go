package proxy

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all configuration parsed from environment variables.
type Config struct {
	// Minecraft listen port.
	MCPort string // e.g. ":25565"

	// Dashboard web port.
	WebPort string // e.g. ":8080"

	// Locale code (en, pt).
	Lang string

	// Multi-server config (loaded from servers.yml). nil = single-server mode.
	Servers     *ServersConfig
	ServersPath string // path to servers.yml, set via env var SERVERS_CONFIG

	// Backend Minecraft server, used in single-server mode.
	BackendTarget string

	// Proxmox connection.
	ProxmoxHost             string
	ProxmoxPort             string
	ProxmoxNode             string
	ProxmoxLXCID            string
	ProxmoxTokenID          string
	ProxmoxTokenSecret      string
	ProxmoxInsecure         bool

	// Crafty Controller connection.
	CraftyHost     string
	CraftyPort     string
	CraftyScheme   string
	CraftyToken    string
	CraftyInsecure bool
	CraftyServerID string

	// Wake-on-LAN.
	WOLMAC       string
	WOLBroadcast string

	// Cooldown in minutes before giving up on a wake attempt.
	CoolDownMinutes int
}

// LoadConfig reads configuration from environment variables.
// Returns an error if any required variable is missing.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		MCPort:             getEnv("MC_LISTEN_PORT", "25565"),
		WebPort:            getEnv("WEB_PORT", "8080"),
		Lang:               getEnv("PROXY_LANG", "en"),
		ServersPath:        getEnv("SERVERS_CONFIG", "servers.yml"),
		BackendTarget:      os.Getenv("BACKEND_TARGET"),
		ProxmoxHost:        os.Getenv("PROXMOX_HOST"),
		ProxmoxPort:        getEnv("PROXMOX_PORT", "8006"),
		ProxmoxNode:        os.Getenv("PROXMOX_NODE"),
		ProxmoxLXCID:       os.Getenv("PROXMOX_LXC_ID"),
		ProxmoxTokenID:     os.Getenv("PROXMOX_TOKEN_ID"),
		ProxmoxTokenSecret:  os.Getenv("PROXMOX_TOKEN_SECRET"),
		ProxmoxInsecure:    getEnvBool("PROXMOX_INSECURE_SKIP_VERIFY", true),
		CraftyHost:         os.Getenv("CRAFTY_HOST"),
		CraftyPort:         getEnv("CRAFTY_PORT", "8443"),
		CraftyScheme:       getEnv("CRAFTY_SCHEME", "https"),
		CraftyToken:        os.Getenv("CRAFTY_TOKEN"),
		CraftyInsecure:     getEnvBool("CRAFTY_INSECURE_SKIP_VERIFY", true),
		CraftyServerID:     os.Getenv("CRAFTY_SERVER_ID"),
		WOLMAC:             os.Getenv("WOL_MAC"),
		WOLBroadcast:       os.Getenv("WOL_BROADCAST"),
		CoolDownMinutes:    getEnvInt("COOLDOWN_MINUTES", 5),
	}

	// Try loading multi-server config.
	servers, err := LoadServers(cfg.ServersPath)
	if err != nil {
		return nil, err
	}
	cfg.Servers = servers

	// Validate required fields.
	missing := []string{}
	if cfg.Servers == nil {
		// Single-server mode: BACKEND_TARGET and CRAFTY_SERVER_ID are required.
		if cfg.BackendTarget == "" {
			missing = append(missing, "BACKEND_TARGET")
		}
		if cfg.CraftyServerID == "" {
			missing = append(missing, "CRAFTY_SERVER_ID")
		}
	}
	if cfg.ProxmoxHost == "" {
		missing = append(missing, "PROXMOX_HOST")
	}
	if cfg.ProxmoxNode == "" {
		missing = append(missing, "PROXMOX_NODE")
	}
	if cfg.ProxmoxLXCID == "" {
		missing = append(missing, "PROXMOX_LXC_ID")
	}
	if cfg.ProxmoxTokenID == "" {
		missing = append(missing, "PROXMOX_TOKEN_ID")
	}
	if cfg.ProxmoxTokenSecret == "" {
		missing = append(missing, "PROXMOX_TOKEN_SECRET")
	}
	if cfg.CraftyHost == "" {
		missing = append(missing, "CRAFTY_HOST")
	}
	if cfg.CraftyToken == "" {
		missing = append(missing, "CRAFTY_TOKEN")
	}
	if cfg.WOLMAC == "" {
		missing = append(missing, "WOL_MAC")
	}
	if cfg.WOLBroadcast == "" {
		missing = append(missing, "WOL_BROADCAST")
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("proxy: missing required env vars: %v", missing)
	}

	// Prefix port strings with colon if not already present.
	if cfg.MCPort[0] != ':' {
		cfg.MCPort = ":" + cfg.MCPort
	}
	if cfg.WebPort[0] != ':' {
		cfg.WebPort = ":" + cfg.WebPort
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}
