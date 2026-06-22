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
	cfg       *Config
	state     *State
	wol       wol.Sender
	proxmox   proxmox.LXCManager
	crafty    crafty.ServerManager
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

	// 1. Read packet length.
	_, err := mcproto.ReadVarInt(client)
	if err != nil {
		return
	}

	// 2. Read packet ID.
	pktID, err := mcproto.ReadVarInt(client)
	if err != nil || pktID != 0x00 {
		return
	}

	// 3. Parse handshake.
	hs, err := mcproto.ReadHandshake(client)
	if err != nil {
		return
	}

	p.state.Logf("MC: handshake from %s → %s (nextState=%d)", client.RemoteAddr(), hs.ServerAddress, hs.NextState)

	switch hs.NextState {
	case 1: // Status (Server List Ping)
		p.handleStatus(client, hs)
	case 2: // Login
		p.handleLogin(client, hs)
	default:
		// Unknown nextState, drop silently.
	}
}

// handleStatus responds to a Server List Ping with MOTD reflecting current state.
func (p *Proxy) handleStatus(client net.Conn, hs *mcproto.Handshake) {
	// Client sends Status Request (0x00) — consume it.
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
	_, _ = client.Write(mcproto.StatusResponse(jsonResp, hs.ProtocolVersion, st.MaxPlayers, st.Players))

	// Handle Ping (0x01) if client sends it.
	_, _ = mcproto.ReadVarInt(client) // length
	pingID, err := mcproto.ReadVarInt(client)
	if err == nil && pingID == 0x01 {
		payload := make([]byte, 8)
		_, _ = io.ReadFull(client, payload)
		_, _ = client.Write(mcproto.PongResponse(payload))
	}
}

// handleLogin handles a player attempting to join.
func (p *Proxy) handleLogin(client net.Conn, hs *mcproto.Handshake) {
	// Consume the Login Start packet.
	packetLen, err := mcproto.ReadVarInt(client)
	if err == nil && packetLen > 0 {
		discard := make([]byte, packetLen)
		_, _ = io.ReadFull(client, discard)
	}

	// If backend is already online, proxy transparently.
	if p.state.IsOnline() {
		p.proxyToBackend(client, hs)
		return
	}

	// If already booting, just kick with "wait" message.
	if p.state.IsBooting() {
		p.kickClient(client, p.state.LangPack().KickBooting)
		return
	}

	// Start wake sequence.
	if !p.state.CanStartWake() {
		p.kickClient(client, p.state.LangPack().KickOffline)
		return
	}

	p.state.SetPhase(PhaseWakingHost)
	p.state.Logf("WAKE: player triggered wake sequence")

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

	// Wait for Proxmox to be reachable.
	for time.Now().Before(deadline) {
		if _, err := p.proxmox.GetLXCStatus(p.cfg.ProxmoxNode, p.cfg.ProxmoxLXCID); err == nil {
			break
		}
		p.state.Logf("PROXMOX: waiting for host to wake...")
		time.Sleep(5 * time.Second)
	}

	// Phase 2: Ensure LXC is running.
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
		p.state.Logf("PROXMOX: LXC did not start within cooldown — giving up")
		p.state.SetOffline()
		return
	}

	// Phase 3: Start Minecraft server via Crafty.
	p.state.SetPhase(PhaseStartingMC)
	p.state.Logf("CRAFTY: starting server %s", p.cfg.CraftyServerID)

	// Give Crafty a moment to be reachable after LXC boot.
	time.Sleep(3 * time.Second)

	if err := p.crafty.StartServer(p.cfg.CraftyServerID); err != nil {
		p.state.Logf("CRAFTY: start error: %v", err)
	} else {
		p.state.Logf("CRAFTY: start_server command sent")
	}

	// Poll Minecraft backend until it answers with a valid connection.
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", p.cfg.BackendTarget, 2*time.Second)
		if err == nil {
			conn.Close()
			p.state.SetOnline()
			p.state.Logf("MC: backend %s is now reachable — proxy ready", p.cfg.BackendTarget)
			return
		}
		p.state.Logf("MC: waiting for backend %s...", p.cfg.BackendTarget)
		time.Sleep(3 * time.Second)
	}

	p.state.Logf("MC: backend did not become reachable within cooldown — giving up")
	p.state.SetOffline()
}

// proxyToBackend connects to the Minecraft backend and transparently forwards bytes.
func (p *Proxy) proxyToBackend(client net.Conn, hs *mcproto.Handshake) {
	backend, err := net.Dial("tcp", p.cfg.BackendTarget)
	if err != nil {
		p.state.Logf("PROXY: backend %s unreachable: %v", p.cfg.BackendTarget, err)
		p.kickClient(client, "§cBackend unreachable — please try again.")
		return
	}
	defer backend.Close()

	p.state.Logf("PROXY: forwarding %s ↔ backend %s", client.RemoteAddr(), p.cfg.BackendTarget)

	// Bidirectional copy.  When one side closes, the other follows.
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(backend, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, backend); done <- struct{}{} }()
	<-done
}

// kickClient sends a Login Disconnect packet with a friendly message.
func (p *Proxy) kickClient(client net.Conn, message string) {
	jsonKick := fmt.Sprintf(`{"text":"%s"}`, escapeJSON(message))
	_, _ = client.Write(mcproto.LoginDisconnect(jsonKick))
}

// escapeJSON escapes double quotes and backslashes for embedding in JSON strings.
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
