// Command mc-wake-proxy is a wake-on-demand proxy for Minecraft servers.
//
// It listens on the Minecraft TCP port and, when a player tries to connect to an
// offline backend, wakes the Proxmox host (WOL), starts the LXC container, and
// launches the Minecraft server via Crafty Controller — all while showing
// friendly MOTD/kick messages to players.
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/mefrraz/mc-wake-proxy/internal/crafty"
	"github.com/mefrraz/mc-wake-proxy/internal/proxmox"
	"github.com/mefrraz/mc-wake-proxy/internal/proxy"
	"github.com/mefrraz/mc-wake-proxy/internal/web"
	"github.com/mefrraz/mc-wake-proxy/internal/wol"
)

func main() {
	cfg, err := proxy.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	state := proxy.NewState(cfg.Lang)
	state.Logf("PROXY: mc-wake-proxy starting (lang=%s)", cfg.Lang)
	state.SetCraftyNode(cfg.CraftyHost + ":" + cfg.CraftyPort)

	// Wire up dependencies.
	wolSender := wol.UDPSender{}
	pmClient := proxmox.NewClient(cfg.ProxmoxHost, cfg.ProxmoxPort, cfg.ProxmoxTokenID, cfg.ProxmoxTokenSecret, cfg.ProxmoxInsecure)
	cmClient := crafty.NewClient(cfg.CraftyHost, cfg.CraftyPort, cfg.CraftyScheme, cfg.CraftyToken, cfg.CraftyInsecure)

	p := proxy.New(cfg, state, wolSender, pmClient, cmClient)

	// Run startup health checks so the user sees what's wrong immediately.
	result := proxy.RunHealthChecks(cfg, pmClient, cmClient)
	for _, hc := range result.Checks {
		if hc.OK {
			state.Logf("HEALTH: %s ✅ — %s", hc.Name, hc.Message)
		} else {
			state.Logf("HEALTH: %s ❌ — %s", hc.Name, hc.Message)
		}
	}
	if !result.AllOK {
		state.Logf("HEALTH: ⚠️  Some checks failed — review the messages above. Proxy will still start.")
	}

	// Store health result in state so /api/health can serve it.
	state.SetHealth(result)

	// If backend is already reachable, start in Ready state.
	for _, hc := range result.Checks {
		if hc.Name == "Backend" && hc.OK {
			state.SetOnline("")
			state.Logf("PROXY: backend already reachable — starting in Ready state")
		}
	}

	// Store server list for dashboard.
	if cfg.Servers != nil {
		state.SetServerEntries(cfg.Servers.Servers)
		if cfg.Servers != nil {
			state.Logf("PROXY: multi-server mode with %d server(s)", len(cfg.Servers.Servers))
		}
	} else {
		state.Logf("PROXY: single-server mode (%s)", cfg.BackendTarget)
	}

	// Load server icon if present.
	if iconData, err := os.ReadFile("server-icon.png"); err == nil {
		state.SetIcon(base64.StdEncoding.EncodeToString(iconData))
		state.Logf("PROXY: loaded server-icon.png (%d bytes)", len(iconData))
	}

	// Crafty discover callback.
	discoverServers := func() ([]proxy.DiscoveredServer, error) {
		list, err := cmClient.ListServers()
		if err != nil {
			return nil, err
		}
		var out []proxy.DiscoveredServer
		for _, s := range list {
			srvCfg, err := cmClient.GetServerConfig(s.ID)
			ip := ""
			port := 25565
			name := s.WorldName
			if err == nil && srvCfg != nil {
				ip = srvCfg.IP
				if srvCfg.Port != 0 { port = srvCfg.Port }
				if srvCfg.Name != "" { name = srvCfg.Name }
			}
			// Crafty often reports 127.0.0.1 — use the LXC's real IP instead.
			if ip == "" || ip == "127.0.0.1" || ip == "0.0.0.0" {
				ip = cfg.CraftyHost
			}
			out = append(out, proxy.DiscoveredServer{
				ID:      s.ID,
				Name:    name,
				IP:      ip,
				Port:    port,
				Players: s.Online,
				Version: s.Version,
				Icon:    s.Icon,
			})
		}
		return out, nil
	}

	// Start dashboard in background.
	go web.Start(state, cfg.WebPort, cfg.ServersPath, cfg.ProxyPassword, p.ReloadServers, cmClient.StopServer, cmClient.RestartServer, cmClient.StartServer, cmClient.SendCommand, discoverServers)

	// Start backend health monitor.
	p.StartMonitor()
	// Start periodic health re-check (every 30s).
	go func() {
		for {
			time.Sleep(30 * time.Second)
			result := proxy.RunHealthChecks(cfg, pmClient, cmClient)
			state.SetHealth(result)
		}
	}()
	// Start auto-shutdown (if configured).
	p.StartAutoShutdown()

	// Run the proxy (blocks).
	state.Logf("PROXY: starting Minecraft listener on %s", cfg.MCPort)
	if err := p.Start(); err != nil {
		state.Logf("PROXY: fatal error: %v", err)
		os.Exit(1)
	}
}
