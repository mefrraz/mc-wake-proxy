package proxy

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/mefrraz/mc-wake-proxy/internal/crafty"
	"github.com/mefrraz/mc-wake-proxy/internal/proxmox"
)

// HealthStatus holds the result of one integration check.
type HealthStatus struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// HealthResult is the full health report.
type HealthResult struct {
	AllOK  bool           `json:"all_ok"`
	Checks []HealthStatus `json:"checks"`
}

// RunHealthChecks tests all external integrations and reports results.
func RunHealthChecks(cfg *Config, pm proxmox.LXCManager, cm crafty.ServerManager) HealthResult {
	var checks []HealthStatus

	// 1. Proxmox
	checks = append(checks, checkProxmox(cfg, pm))

	// 2. Crafty
	checks = append(checks, checkCrafty(cfg, cm))

	// 3. WOL
	checks = append(checks, checkWOL(cfg))

	// 4. Backend
	checks = append(checks, checkBackend(cfg))

	allOK := true
	for _, c := range checks {
		if !c.OK {
			allOK = false
		}
	}

	return HealthResult{AllOK: allOK, Checks: checks}
}

func checkProxmox(cfg *Config, pm proxmox.LXCManager) HealthStatus {
	status, err := pm.GetLXCStatus(cfg.ProxmoxNode, cfg.ProxmoxLXCID)
	if err != nil {
		msg := err.Error()
		hint := diagnoseProxmoxError(err)
		if hint != "" {
			msg += "\n💡 " + hint
		}
		return HealthStatus{Name: "Proxmox", OK: false, Message: msg}
	}
	return HealthStatus{
		Name:    "Proxmox",
		OK:      true,
		Message: fmt.Sprintf("LXC %s is %s on node %s", cfg.ProxmoxLXCID, status.Status, cfg.ProxmoxNode),
	}
}

func checkCrafty(cfg *Config, cm crafty.ServerManager) HealthStatus {
	// Multi-server: check all configured servers.
	if cfg.Servers != nil && len(cfg.Servers.Servers) > 0 {
		var lastErr error
		for _, s := range cfg.Servers.Servers {
			info, err := cm.GetServerStatus(s.CraftyServerID)
			if err == nil {
				status := "stopped"
				if info.Running { status = "running" }
				return HealthStatus{Name: "Crafty", OK: true, Message: fmt.Sprintf("%s is %s (%d players)", s.Hostname, status, info.Online)}
			}
			lastErr = err
		}
		return HealthStatus{Name: "Crafty", OK: false, Message: fmt.Sprintf("All servers failed: %v", lastErr)}
	}
	info, err := cm.GetServerStatus(cfg.CraftyServerID)
	if err != nil {
		msg := err.Error()
		hint := diagnoseCraftyError(err)
		if hint != "" {
			msg += "\n💡 " + hint
		}
		return HealthStatus{Name: "Crafty", OK: false, Message: msg}
	}
	status := "stopped"
	if info.Running {
		status = "running"
	}
	return HealthStatus{
		Name:    "Crafty",
		OK:      true,
		Message: fmt.Sprintf("Server %s is %s (%d players online)", cfg.CraftyServerID[:8], status, info.Online),
	}
}

func checkWOL(cfg *Config) HealthStatus {
	mac := strings.NewReplacer(":", "", "-", "").Replace(cfg.WOLMAC)
	if len(mac) != 12 {
		return HealthStatus{
			Name:    "Wake-on-LAN",
			OK:      false,
			Message: fmt.Sprintf("Invalid MAC length (%d chars, expected 12). Current: %s", len(mac), cfg.WOLMAC),
		}
	}
	// Try sending a packet — won't confirm receipt, but validates the broadcast address is reachable.
	// Just check the broadcast address format.
	if net.ParseIP(cfg.WOLBroadcast) == nil {
		return HealthStatus{
			Name:    "Wake-on-LAN",
			OK:      false,
			Message: fmt.Sprintf("Invalid broadcast IP: %s", cfg.WOLBroadcast),
		}
	}
	return HealthStatus{
		Name:    "Wake-on-LAN",
		OK:      true,
		Message: fmt.Sprintf("MAC %s, broadcast %s — ready", cfg.WOLMAC, cfg.WOLBroadcast),
	}
}

func checkBackend(cfg *Config) HealthStatus {
	// Multi-server: check all configured backends.
	if cfg.Servers != nil && len(cfg.Servers.Servers) > 0 {
		for _, s := range cfg.Servers.Servers {
			conn, err := net.DialTimeout("tcp", s.Backend, 3*time.Second)
			if err == nil {
				conn.Close()
				return HealthStatus{Name: "Backend", OK: true, Message: fmt.Sprintf("%s reachable", s.Backend)}
			}
		}
		return HealthStatus{Name: "Backend", OK: false, Message: "No backends reachable"}
	}
	conn, err := net.DialTimeout("tcp", cfg.BackendTarget, 3*time.Second)
	if err != nil {
		return HealthStatus{
			Name:    "Backend",
			OK:      false,
			Message: fmt.Sprintf("%s is not reachable: %v (expected if server is offline)", cfg.BackendTarget, err),
		}
	}
	conn.Close()
	return HealthStatus{
		Name:    "Backend",
		OK:      true,
		Message: fmt.Sprintf("%s is reachable — Minecraft server is online", cfg.BackendTarget),
	}
}

// diagnoseProxmoxError returns a user-friendly hint based on the error message.
func diagnoseProxmoxError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "403") || strings.Contains(msg, "Permission check failed"):
		return "API token exists but lacks permissions. In Proxmox UI: Datacenter → Permissions → Add → API Token Permission → Path /vms/" + "ID" + " → assign PVEVMUser role."
	case strings.Contains(msg, "401") || strings.Contains(msg, "authentication"):
		return "API token rejected. Check PROXMOX_TOKEN_ID and PROXMOX_TOKEN_SECRET."
	case strings.Contains(msg, "lookup") || strings.Contains(msg, "no such host") || strings.Contains(msg, "Name or service not known"):
		return "Proxmox node name is wrong. Run 'hostname' on the Proxmox host and use that value for PROXMOX_NODE."
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "timeout") || strings.Contains(msg, "no route"):
		return "Cannot reach Proxmox host. Is " + "PROXMOX_HOST" + " correct? Is the Proxmox host powered on? Is port 8006 open?"
	case strings.Contains(msg, "certificate") || strings.Contains(msg, "x509"):
		return "TLS error. Set PROXMOX_INSECURE_SKIP_VERIFY=true for self-signed certs."
	default:
		return ""
	}
}

// diagnoseCraftyError returns a user-friendly hint based on the error message.
func diagnoseCraftyError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found in status response"):
		return "Server ID not found. Check CRAFTY_SERVER_ID. In Crafty → Server Detail → URL shows the ID. Also ensure 'Show Status' is ON for this server."
	case strings.Contains(msg, "401") || strings.Contains(msg, "403") || strings.Contains(msg, "NOT_AUTHORIZED"):
		return "API token rejected. Check CRAFTY_TOKEN."
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "timeout") || strings.Contains(msg, "no route"):
		return "Cannot reach Crafty. Check CRAFTY_HOST and CRAFTY_PORT. Is the LXC running?"
	case strings.Contains(msg, "certificate") || strings.Contains(msg, "x509"):
		return "TLS error. Set CRAFTY_INSECURE_SKIP_VERIFY=true or use CRAFTY_SCHEME=http."
	default:
		return ""
	}
}
