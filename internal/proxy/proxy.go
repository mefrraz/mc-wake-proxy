package proxy

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/mefrraz/mc-wake-proxy/internal/crafty"
	"github.com/mefrraz/mc-wake-proxy/internal/mcproto"
	"github.com/mefrraz/mc-wake-proxy/internal/proxmox"
	"github.com/mefrraz/mc-wake-proxy/internal/wol"
)

// JSON templates for Minecraft status responses.
const statusJSON = `{"version":{"name":"mc-wake-proxy","protocol":%d},"players":{"max":%d,"online":%d},"description":{"text":"%s"}%s}`

// Proxy orchestrates the wake-on-demand lifecycle and transparent TCP forwarding.
type Proxy struct {
	cfg     *Config
	state   *State
	wol     wol.Sender
	proxmox proxmox.LXCManager
	crafty  crafty.ServerManager
}

// New creates a Proxy with concrete clients.
func New(cfg *Config, state *State, wolSender wol.Sender, pm proxmox.LXCManager, cm crafty.ServerManager) *Proxy {
	return &Proxy{
		cfg:     cfg,
		state:   state,
		wol:     wolSender,
		proxmox: pm,
		crafty:  cm,
	}
}

// Start begins listening for Minecraft connections on cfg.MCPort.
// It blocks until the listener fails.
func (p *Proxy) Start() error {
	l, err := net.Listen("tcp", p.cfg.MCPort)
	if err != nil {
		return fmt.Errorf("proxy: listen on %s: %w", p.cfg.MCPort, err)
	}
	defer l.Close()
	p.state.Logf("PROXY: listening on %s", p.cfg.MCPort)

	for {
		conn, err := l.Accept()
		if err != nil {
			p.state.Logf("PROXY: accept error: %v", err)
			continue
		}
		go p.handleConnection(conn)
	}
}

// StartMonitor runs a background loop that checks backend reachability.
// If the backend goes down while the proxy thinks it's online, the state is
// updated so the next player triggers a wake sequence.
func (p *Proxy) StartMonitor() {
	go func() {
		for {
			time.Sleep(5 * time.Second)
			if !p.state.IsOnline("") && !p.state.HasAnyOnline() {
				continue
			}
			// Check global backend in single-server mode, or all backends in multi mode.
			if p.cfg.Servers != nil {
				for _, srv := range p.cfg.Servers.Servers {
					if p.state.IsOnline(srv.Hostname) {
						conn, err := net.DialTimeout("tcp", srv.Backend, 2*time.Second)
						if err != nil {
							p.state.SetOffline(srv.Hostname)
							p.state.Logf("MONITOR: %s (%s) went offline", srv.Hostname, srv.Backend)
						} else {
							conn.Close()
						}
					}
				}
			} else {
				conn, err := net.DialTimeout("tcp", p.cfg.BackendTarget, 2*time.Second)
				if err != nil {
					p.state.SetOfflineGlobally()
					p.state.Logf("MONITOR: backend %s went offline", p.cfg.BackendTarget)
				} else {
					conn.Close()
				}
			}
		}
	}()
}

// ReloadServers re-reads servers.yml and updates the proxy's routing table.
func (p *Proxy) ReloadServers(path string) error {
	sc, err := LoadServers(path)
	if err != nil {
		return err
	}
	p.cfg.Servers = sc
	if sc != nil {
		p.state.SetServerEntries(sc.Servers)
		// Check which backends are already reachable.
		for _, srv := range sc.Servers {
			conn, err := net.DialTimeout("tcp", srv.Backend, 2*time.Second)
			if err == nil {
				conn.Close()
				p.state.SetOnline(srv.Hostname)
				p.state.Logf("PROXY: %s is online (%s)", srv.Hostname, srv.Backend)
			} else {
				p.state.Logf("PROXY: %s is offline (%s: %v)", srv.Hostname, srv.Backend, err)
			}
		}
		p.state.Logf("PROXY: reloaded %d server(s) from %s", len(sc.Servers), path)
	} else {
		p.state.SetServerEntries(nil)
		p.state.Logf("PROXY: servers.yml not found — switched to single-server mode")
	}
	return nil
}

// ConfigPath returns the servers config path.
func (p *Proxy) ConfigPath() string { return p.cfg.ServersPath }

// StartAutoShutdown runs a background loop that stops idle servers.
// If AUTO_SHUTDOWN_MINUTES > 0 and a server has 0 players for that duration,
// the server is stopped via Crafty.
func (p *Proxy) StartAutoShutdown() {
	interval := time.Duration(p.cfg.AutoShutdownMinutes) * time.Minute
	if interval <= 0 {
		return
	}
	p.state.Logf("AUTO-SHUTDOWN: enabled (%d min idle timeout)", p.cfg.AutoShutdownMinutes)

	// Track how long each server has been empty.
	idleSince := make(map[string]time.Time)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			checkServer := func(hostname, craftyID string) {
				info, err := p.crafty.GetServerStatus(craftyID)
				if err != nil || !info.Running {
					delete(idleSince, hostname)
					return
				}
				if info.Online > 0 {
					delete(idleSince, hostname)
					return
				}
				// Zero players — start or continue tracking.
				if since, ok := idleSince[hostname]; ok {
					if time.Since(since) >= interval {
						p.state.Logf("AUTO-SHUTDOWN: %s idle for %v — stopping", hostname, time.Since(since).Round(time.Minute))
						if err := p.crafty.StopServer(craftyID); err != nil {
							p.state.Logf("AUTO-SHUTDOWN: stop %s failed: %v", hostname, err)
						} else {
							p.state.SetOffline(hostname)
							delete(idleSince, hostname)
						}
					}
				} else {
					idleSince[hostname] = time.Now()
				}
			}

			if p.cfg.Servers != nil {
				for _, srv := range p.cfg.Servers.Servers {
					checkServer(srv.Hostname, srv.CraftyServerID)
				}
			} else {
				checkServer("", p.cfg.CraftyServerID)
			}
		}
	}()
}

// resolveServer returns the backend and crafty_server_id for a hostname.
// In multi-server mode, looks up the server entry. Falls back to global config.
func (p *Proxy) resolveServer(hostname string) (backend, craftyServerID string) {
	if p.cfg.Servers != nil {
		if entry := p.cfg.Servers.Lookup(hostname); entry != nil {
			return entry.Backend, entry.CraftyServerID
		}
	}
	return p.cfg.BackendTarget, p.cfg.CraftyServerID
}
func (p *Proxy) handleConnection(client net.Conn) {
	defer client.Close()

	// 1. Read the full handshake packet (length + body) as raw bytes for replay.
	hsRaw, err := mcproto.ReadPacketRaw(client)
	if err != nil {
		return
	}

	// 2. Strip the length prefix properly, then parse.
	hsBody, err := mcproto.StripPacketPrefix(hsRaw)
	if err != nil {
		return
	}
	hs, err := mcproto.ParseHandshake(hsBody)
	if err != nil {
		return
	}

	playerAddr := client.RemoteAddr().String()
	p.state.Logf("MC: handshake from %s → %s (nextState=%d)", playerAddr, hs.ServerAddress, hs.NextState)

	switch hs.NextState {
	case 1: // Status (Server List Ping)
		p.handleStatus(client, hs)
	case 2: // Login
		lsRaw, err := mcproto.ReadPacketRaw(client)
		p.handleLogin(client, hs, hsRaw, lsRaw, err)
	default:
	}
}

// handleStatus responds to a Server List Ping with MOTD reflecting current state.
func (p *Proxy) handleStatus(client net.Conn, hs *mcproto.Handshake) {
	_, _ = mcproto.ReadVarInt(client)
	_, _ = mcproto.ReadVarInt(client)

	lp := p.state.LangPack()
	phase := p.state.Phase()

	var motd string
	switch phase {
	case PhaseReady:
		motd = lp.MotdReady
	default:
		if p.state.IsBooting() {
			motd = lp.MotdBooting
		} else {
			motd = lp.MotdOffline
		}
	}

	st := p.state.Status()
	// Build icon JSON fragment if available.
	iconJSON := ""
	if icon := p.state.Icon(); icon != "" {
		iconJSON = fmt.Sprintf(`,"favicon":"data:image/png;base64,%s"`, icon)
	}
	jsonResp := fmt.Sprintf(statusJSON, hs.ProtocolVersion, st.MaxPlayers, st.Players, escapeJSON(motd), iconJSON)
	_, _ = client.Write(mcproto.StatusResponse(jsonResp))

	_, _ = mcproto.ReadVarInt(client)
	pingID, err := mcproto.ReadVarInt(client)
	if err == nil && pingID == 0x01 {
		payload := make([]byte, 8)
		_, _ = io.ReadFull(client, payload)
		_, _ = client.Write(mcproto.PongResponse(payload))
	}
}

// handleLogin handles a player attempting to join.
func (p *Proxy) handleLogin(client net.Conn, hs *mcproto.Handshake, hsRaw, lsRaw []byte, lsErr error) {
	player := "unknown"
	if lsErr == nil {
		if lsBody, err := mcproto.StripPacketPrefix(lsRaw); err == nil {
			if name, err := mcproto.ParseLoginStart(lsBody); err == nil {
				player = name
			}
		}
	}

	hostname := hs.ServerAddress
	backend, craftyID := p.resolveServer(hostname)

	// If this specific backend is already online, proxy transparently.
	if p.state.IsOnline(hostname) {
		p.state.Logf("MC: %s joining %s — backend online, proxying", player, hostname)
		p.proxyToBackend(client, hsRaw, lsRaw, backend)
		return
	}

	// If already booting, kick.
	if p.state.IsBooting() {
		p.kickClient(client, p.state.LangPack().KickBooting)
		return
	}

	if !p.state.CanStartWake() {
		p.kickClient(client, p.state.LangPack().KickOffline)
		return
	}

	p.state.SetPhase(PhaseWakingHost)
	p.state.Logf("WAKE: %s triggered wake for %s", player, hostname)

	go p.wakeSequence(hostname, backend, craftyID)

	p.kickClient(client, p.state.LangPack().KickOffline)
}

// wakeSequence runs the Proxmox → LXC → Crafty → Minecraft chain for a specific server.
func (p *Proxy) wakeSequence(hostname, backend, craftyServerID string) {
	cooldown := time.Duration(p.cfg.CoolDownMinutes) * time.Minute
	deadline := time.Now().Add(cooldown)

	// Phase 1: Ensure Proxmox host is reachable.
	p.state.SetPhase(PhaseWakingHost)
	_, err := p.proxmox.GetLXCStatus(p.cfg.ProxmoxNode, p.cfg.ProxmoxLXCID)
	if err == nil {
		p.state.Logf("PROXMOX: host already reachable — skipping WOL")
	} else {
		p.state.Logf("PROXMOX: host not reachable (%v) — sending WOL to %s", err, p.cfg.WOLMAC)
		if werr := p.wol.Send(p.cfg.WOLMAC, p.cfg.WOLBroadcast); werr != nil {
			p.state.Logf("WOL: error: %v", werr)
		} else {
			p.state.Logf("WOL: magic packet sent")
		}
		firstFail := true
		for time.Now().Before(deadline) {
			_, err := p.proxmox.GetLXCStatus(p.cfg.ProxmoxNode, p.cfg.ProxmoxLXCID)
			if err == nil {
				p.state.Logf("PROXMOX: host is now reachable")
				break
			}
			if firstFail {
				p.state.Logf("PROXMOX: error: %v", err)
				firstFail = false
			}
			p.state.Logf("PROXMOX: waiting for host to wake...")
			time.Sleep(5 * time.Second)
		}
	}

	// Phase 2: Ensure LXC is running.
	p.state.SetPhase(PhaseWaitingLXC)
	lxcRunning := false
	for time.Now().Before(deadline) {
		status, err := p.proxmox.GetLXCStatus(p.cfg.ProxmoxNode, p.cfg.ProxmoxLXCID)
		if err != nil {
			p.state.Logf("PROXMOX: LXC status error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if status.Status == "running" {
			p.state.Logf("PROXMOX: LXC already running — skipping start")
			lxcRunning = true
			break
		}
		p.state.Logf("PROXMOX: LXC is %s — starting...", status.Status)
		if _, err := p.proxmox.StartLXC(p.cfg.ProxmoxNode, p.cfg.ProxmoxLXCID); err != nil {
			p.state.Logf("PROXMOX: start LXC error: %v", err)
		}
		time.Sleep(5 * time.Second)
	}
	if !lxcRunning {
		p.state.Logf("PROXMOX: LXC did not start — giving up")
		p.state.SetOffline(hostname)
		return
	}

	// Phase 3: Ensure Minecraft server is running via Crafty.
	p.state.SetPhase(PhaseStartingMC)
	info, err := p.crafty.GetServerStatus(craftyServerID)
	if err == nil && info.Running {
		p.state.Logf("CRAFTY: server %s already running — skipping start", craftyServerID[:8])
	} else {
		p.state.Logf("CRAFTY: starting server %s", craftyServerID[:8])
		time.Sleep(3 * time.Second)
		if err := p.crafty.StartServer(craftyServerID); err != nil {
			p.state.Logf("CRAFTY: start error: %v", err)
		} else {
			p.state.Logf("CRAFTY: start_server command sent")
		}
	}

	// Poll until Minecraft backend is reachable.
	for time.Now().Before(deadline) {
		info, err := p.crafty.GetServerStatus(craftyServerID)
		if err == nil && info.Running {
			conn, err := net.DialTimeout("tcp", backend, 2*time.Second)
			if err == nil {
				conn.Close()
				p.state.UpdatePlayers(info.Online, nil)
				p.state.SetOnline(hostname)
				p.state.Logf("MC: backend %s ready (%d players)", backend, info.Online)
				return
			}
			p.state.Logf("MC: Crafty says running but TCP failed — retrying...")
		} else {
			p.state.Logf("MC: waiting for server...")
		}
		time.Sleep(3 * time.Second)
	}

	p.state.Logf("MC: backend not reachable — giving up")
	p.state.SetOffline(hostname)
}

// proxyToBackend replays captured handshake + login bytes, then transparently forwards.
func (p *Proxy) proxyToBackend(client net.Conn, hsRaw, lsRaw []byte, backend string) {
	backendConn, err := net.Dial("tcp", backend)
	if err != nil {
		p.state.Logf("PROXY: backend %s unreachable: %v", backend, err)
		p.state.SetOffline("")
		if p.state.CanStartWake() {
			p.state.SetPhase(PhaseWakingHost)
			go p.wakeSequence("", backend, p.cfg.CraftyServerID)
		}
		p.kickClient(client, p.state.LangPack().KickOffline)
		return
	}
	defer backendConn.Close()

	if _, err := backendConn.Write(hsRaw); err != nil {
		p.state.Logf("PROXY: replay handshake failed: %v", err)
		p.kickClient(client, "§cConnection error — please try again.")
		return
	}
	if _, err := backendConn.Write(lsRaw); err != nil {
		p.state.Logf("PROXY: replay login start failed: %v", err)
		p.kickClient(client, "§cConnection error — please try again.")
		return
	}

	p.state.Logf("PROXY: forwarding %s ↔ backend %s", client.RemoteAddr(), backend)

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(backendConn, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, backendConn); done <- struct{}{} }()
	<-done
}

// kickClient sends a Login Disconnect packet.
func (p *Proxy) kickClient(client net.Conn, message string) {
	jsonKick := fmt.Sprintf(`{"text":"%s"}`, escapeJSON(message))
	_, _ = client.Write(mcproto.LoginDisconnect(jsonKick))
}

// escapeJSON escapes double quotes and backslashes for JSON strings.
func escapeJSON(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			result = append(result, '\\', '"')
		case '\\':
			result = append(result, '\\', '\\')
		default:
			result = append(result, s[i])
		}
	}
	return string(result)
}
