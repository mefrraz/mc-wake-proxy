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
const statusJSON = `{"version":{"name":"mc-wake-proxy","protocol":%d},"players":{"max":%d,"online":%d},"description":{"text":"%s"}}`

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

// handleConnection processes one Minecraft client connection.
func (p *Proxy) handleConnection(client net.Conn) {
	defer client.Close()

	// 1. Read the full handshake packet (length + body) as raw bytes for replay.
	hsRaw, err := mcproto.ReadPacketRaw(client)
	if err != nil {
		return
	}

	// 2. Parse handshake from the raw body (skip the leading length VarInt byte(s)).
	hs, err := mcproto.ParseHandshake(hsRaw[1:])
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
	jsonResp := fmt.Sprintf(statusJSON, hs.ProtocolVersion, st.MaxPlayers, st.Players, escapeJSON(motd))
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
	player := extractPlayerName(lsRaw)

	// If backend is already online, proxy transparently.
	if p.state.IsOnline() {
		p.state.Logf("MC: %s joining — backend online, proxying", player)
		p.proxyToBackend(client, hsRaw, lsRaw)
		return
	}

	// If already booting, just kick.
	if p.state.IsBooting() {
		p.kickClient(client, p.state.LangPack().KickBooting)
		return
	}

	if !p.state.CanStartWake() {
		p.kickClient(client, p.state.LangPack().KickOffline)
		return
	}

	p.state.SetPhase(PhaseWakingHost)
	p.state.Logf("WAKE: %s triggered wake sequence", player)

	go p.wakeSequence()

	p.kickClient(client, p.state.LangPack().KickOffline)
}

// wakeSequence runs the full Proxmox → LXC → Crafty → Minecraft chain.
func (p *Proxy) wakeSequence() {
	cooldown := time.Duration(p.cfg.CoolDownMinutes) * time.Minute
	deadline := time.Now().Add(cooldown)

	// Phase 1: Wake Proxmox host.
	p.state.SetPhase(PhaseWakingHost)
	p.state.Logf("WOL: sending magic packet to %s via %s", p.cfg.WOLMAC, p.cfg.WOLBroadcast)
	if err := p.wol.Send(p.cfg.WOLMAC, p.cfg.WOLBroadcast); err != nil {
		p.state.Logf("WOL: error: %v", err)
	} else {
		p.state.Logf("WOL: magic packet sent")
	}

	for time.Now().Before(deadline) {
		if _, err := p.proxmox.GetLXCStatus(p.cfg.ProxmoxNode, p.cfg.ProxmoxLXCID); err == nil {
			break
		}
		p.state.Logf("PROXMOX: waiting for host to wake...")
		time.Sleep(5 * time.Second)
	}

	// Phase 2: Ensure LXC running.
	p.state.SetPhase(PhaseWaitingLXC)
	p.state.Logf("PROXMOX: checking LXC %s on node %s", p.cfg.ProxmoxLXCID, p.cfg.ProxmoxNode)

	lxcRunning := false
	for time.Now().Before(deadline) {
		status, err := p.proxmox.GetLXCStatus(p.cfg.ProxmoxNode, p.cfg.ProxmoxLXCID)
		if err != nil {
			p.state.Logf("PROXMOX: LXC status error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if status.Status == "running" {
			lxcRunning = true
			p.state.Logf("PROXMOX: LXC is running")
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
		p.state.SetOffline()
		return
	}

	// Phase 3: Start Minecraft via Crafty, poll with Crafty API.
	p.state.SetPhase(PhaseStartingMC)
	p.state.Logf("CRAFTY: starting server %s", p.cfg.CraftyServerID)

	time.Sleep(3 * time.Second) // let Crafty be reachable

	if err := p.crafty.StartServer(p.cfg.CraftyServerID); err != nil {
		p.state.Logf("CRAFTY: start error: %v", err)
	} else {
		p.state.Logf("CRAFTY: start_server command sent")
	}

	for time.Now().Before(deadline) {
		info, err := p.crafty.GetServerStatus(p.cfg.CraftyServerID)
		if err == nil && info.Running {
			conn, err := net.DialTimeout("tcp", p.cfg.BackendTarget, 2*time.Second)
			if err == nil {
				conn.Close()
				p.state.UpdatePlayers(info.Online, nil)
				p.state.SetOnline()
				p.state.Logf("MC: backend %s ready (%d players)", p.cfg.BackendTarget, info.Online)
				return
			}
			p.state.Logf("MC: Crafty says running but TCP failed — retrying...")
		} else {
			p.state.Logf("MC: waiting for server...")
		}
		time.Sleep(3 * time.Second)
	}

	p.state.Logf("MC: backend not reachable — giving up")
	p.state.SetOffline()
}

// proxyToBackend replays captured handshake + login bytes, then transparently forwards.
func (p *Proxy) proxyToBackend(client net.Conn, hsRaw, lsRaw []byte) {
	backend, err := net.Dial("tcp", p.cfg.BackendTarget)
	if err != nil {
		p.state.Logf("PROXY: backend %s unreachable: %v", p.cfg.BackendTarget, err)
		p.kickClient(client, "§cBackend unreachable — please try again.")
		return
	}
	defer backend.Close()

	// Replay captured packets so the backend sees the handshake + login.
	if _, err := backend.Write(hsRaw); err != nil {
		p.state.Logf("PROXY: replay handshake failed: %v", err)
		return
	}
	if _, err := backend.Write(lsRaw); err != nil {
		p.state.Logf("PROXY: replay login start failed: %v", err)
		return
	}

	p.state.Logf("PROXY: forwarding %s ↔ backend %s", client.RemoteAddr(), p.cfg.BackendTarget)

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(backend, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, backend); done <- struct{}{} }()
	<-done
}

// kickClient sends a Login Disconnect packet.
func (p *Proxy) kickClient(client net.Conn, message string) {
	jsonKick := fmt.Sprintf(`{"text":"%s"}`, escapeJSON(message))
	_, _ = client.Write(mcproto.LoginDisconnect(jsonKick))
}

// extractPlayerName extracts the player name from raw Login Start bytes.
func extractPlayerName(lsRaw []byte) string {
	if len(lsRaw) < 3 {
		return "unknown"
	}
	body := lsRaw[1:] // skip first length VarInt byte
	for i := 0; i < len(body) && i < 5; i++ {
		if body[i]&0x80 == 0 {
			if i+2 >= len(body) {
				return "unknown"
			}
			rest := body[i+2:] // skip packet ID byte
			name, err := mcproto.ReadStringFromBytes(rest)
			if err != nil || name == "" {
				return "unknown"
			}
			return name
		}
	}
	return "unknown"
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
