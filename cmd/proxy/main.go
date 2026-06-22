// Command mc-wake-proxy is a wake-on-demand proxy for Minecraft servers.
//
// It listens on the Minecraft TCP port and, when a player tries to connect to an
// offline backend, wakes the Proxmox host (WOL), starts the LXC container, and
// launches the Minecraft server via Crafty Controller — all while showing
// friendly MOTD/kick messages to players.
package main

import (
	"fmt"
	"os"

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

	// Wire up dependencies.
	wolSender := wol.UDPSender{}
	pmClient := proxmox.NewClient(cfg.ProxmoxHost, cfg.ProxmoxPort, cfg.ProxmoxTokenID, cfg.ProxmoxTokenSecret, cfg.ProxmoxInsecure)
	cmClient := crafty.NewClient(cfg.CraftyHost, cfg.CraftyPort, cfg.CraftyToken, cfg.CraftyInsecure)

	p := proxy.New(cfg, state, wolSender, pmClient, cmClient)

	// Start dashboard in background.
	go web.Start(state, cfg.WebPort)

	// Run the proxy (blocks).
	state.Logf("PROXY: starting Minecraft listener on %s", cfg.MCPort)
	if err := p.Start(); err != nil {
		state.Logf("PROXY: fatal error: %v", err)
		os.Exit(1)
	}
}
