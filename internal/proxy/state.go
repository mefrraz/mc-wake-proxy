// Package proxy implements the core wake-on-demand proxy state machine and TCP forwarding.
package proxy

import (
	"fmt"
	"sync"
	"time"
)

// Phase represents a step in the wake-flow state machine.
type Phase string

const (
	PhaseIdle       Phase = "idle"
	PhaseWakingHost Phase = "waking_host"
	PhaseWaitingLXC Phase = "waiting_lxc"
	PhaseStartingMC Phase = "starting_mc"
	PhaseReady      Phase = "ready"
)

// ServerStatus holds the current runtime status exposed to the dashboard.
type ServerStatus struct {
	Phase        Phase     `json:"phase"`
	PhaseSince   time.Time `json:"phase_since"`
	ServerOnline bool      `json:"server_online"`
	Players      int       `json:"players"`
	MaxPlayers   int       `json:"max_players"`
	PlayerList   []string  `json:"player_list"`
	WorldSeed    string    `json:"world_seed"`
	WorldName    string    `json:"world_name"`
}

// LangPack holds translated MOTD and kick messages for a locale.
type LangPack struct {
	MotdOffline  string
	MotdBooting  string
	MotdReady    string
	KickOffline  string
	KickBooting  string
}

var defaultLang = LangPack{
	MotdOffline:  "§7● §cServer Offline §7- §eClick to start",
	MotdBooting:  "§7● §eStarting server... §7[§bPlease wait§7]",
	MotdReady:    "§7● §aServer Online §7- §eJoin now!",
	KickOffline:  "§6Server is waking up!\n\n§7The startup signal was sent.\n§ePlease wait 1-2 minutes and reconnect.",
	KickBooting:  "§eServer is still loading...\n\n§7The world is initializing.\n§fRefresh your server list and join once ready!",
}

var locales = map[string]LangPack{
	"pt": {
		MotdOffline:  "§7● §cServidor Desligado §7- §eClica para iniciar",
		MotdBooting:  "§7● §eA iniciar o servidor... §7[§bAguarde§7]",
		MotdReady:    "§7● §aServidor Online §7- §eEntra já!",
		KickOffline:  "§6O servidor está a ser iniciado!\n\n§7Enviámos o sinal de arranque.\n§ePor favor, aguarda 1 a 2 minutos e volta a entrar.",
		KickBooting:  "§eO servidor ainda está a carregar...\n\n§7O mundo está a iniciar.\n§fAtualiza a lista e entra assim que estiver pronto!",
	},
}

// serverState tracks the runtime status of a single Minecraft backend.
type serverState struct {
	online bool
	phase  Phase
	phaseSince time.Time
	players    int
	playerList []string
	logs       []string
}

// State is the global proxy state, safe for concurrent use.
type State struct {
	mu sync.RWMutex

	// Shared wake phases (Proxmox, LXC).
	phase      Phase
	phaseSince time.Time

	// Global defaults (used in single-server mode or as fallback).
	serverOnline bool
	players      int
	maxPlayers   int
	playerList   []string
	worldSeed    string
	worldName    string

	// Per-server state keyed by hostname (multi-server mode).
	servers map[string]*serverState

	logs      []string
	logMaxLen int

	lang   string
	health HealthResult

	// Server icon base64-encoded PNG (for MOTD).
	iconBase64 string

	// Server config list (for dashboard / API).
	serverEntries []ServerEntry
}

// NewState returns an initialized State with the given locale.
func NewState(lang string) *State {
	s := &State{
		phase:      PhaseIdle,
		phaseSince: time.Now(),
		maxPlayers: 20,
		worldName:  "world",
		worldSeed:  "N/A",
		playerList: []string{},
		servers:    make(map[string]*serverState),
		logMaxLen:  200,
		lang:       lang,
	}
	return s
}

// --- Phase management ---

// Phase returns the current wake-flow phase.
func (s *State) Phase() Phase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.phase
}

// SetPhase transitions to a new phase and records the timestamp.
func (s *State) SetPhase(p Phase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.phase = p
	s.phaseSince = time.Now()
}

// PhaseSince returns when the current phase started.
func (s *State) PhaseSince() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.phaseSince
}

// PhaseElapsed returns how long the current phase has been active.
func (s *State) PhaseElapsed() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.phaseSince)
}

// CanStartWake returns true if the proxy is not already in a wake sequence and the server is not online.
func (s *State) CanStartWake() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.phase == PhaseIdle && !s.serverOnline
}

// IsOnline returns true if a specific backend is online.
// hostname is the server's hostname (empty = global default).
func (s *State) IsOnline(hostname string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ss, ok := s.servers[hostname]; ok {
		return ss.online
	}
	return s.serverOnline
}

// IsBooting returns true if a wake sequence is in progress.
func (s *State) IsBooting() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.phase != PhaseIdle && s.phase != PhaseReady
}

// --- Server status ---

// SetOnline marks a backend as online.
func (s *State) SetOnline(hostname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serverOnline = true
	s.phase = PhaseReady
	s.phaseSince = time.Now()
	if ss := s.ensureServer(hostname); ss != nil {
		ss.online = true
		ss.phase = PhaseReady
		ss.phaseSince = time.Now()
	}
}

// SetOffline marks a backend as offline.
func (s *State) SetOffline(hostname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Only update global if this was the server keeping it online.
	if ss, ok := s.servers[hostname]; ok {
		ss.online = false
		ss.players = 0
		ss.playerList = nil
		ss.phase = PhaseIdle
		ss.phaseSince = time.Now()
	}
	// Check if any server is still online before resetting global.
	anyOnline := false
	for _, ss := range s.servers {
		if ss.online {
			anyOnline = true
			break
		}
	}
	if !anyOnline {
		s.serverOnline = false
		s.players = 0
		s.playerList = []string{}
		s.phase = PhaseIdle
		s.phaseSince = time.Now()
	}
}

// SetOfflineGlobally resets all state (used by monitor).
func (s *State) SetOfflineGlobally() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serverOnline = false
	s.players = 0
	s.playerList = []string{}
	s.phase = PhaseIdle
	s.phaseSince = time.Now()
	for _, ss := range s.servers {
		ss.online = false
		ss.phase = PhaseIdle
		ss.phaseSince = time.Now()
	}
}

// ensureServer returns the serverState for hostname, creating it if needed.
// Must be called with mu held (write lock).
func (s *State) ensureServer(hostname string) *serverState {
	if hostname == "" {
		return nil
	}
	ss, ok := s.servers[hostname]
	if !ok {
		ss = &serverState{phase: PhaseIdle, phaseSince: time.Now()}
		s.servers[hostname] = ss
	}
	return ss
}

// HasAnyOnline returns true if at least one server is marked online.
func (s *State) HasAnyOnline() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ss := range s.servers {
		if ss.online {
			return true
		}
	}
	return s.serverOnline
}

// ServerStatuses returns a map of hostname → online status for the dashboard.
func (s *State) ServerStatuses() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]bool)
	for host, ss := range s.servers {
		out[host] = ss.online
	}
	return out
}

// SetServerEntries stores the configured server list.
func (s *State) SetServerEntries(entries []ServerEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serverEntries = entries
}

// ServerEntries returns the configured server list.
func (s *State) ServerEntries() []ServerEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ServerEntry, len(s.serverEntries))
	copy(out, s.serverEntries)
	return out
}

// SetIcon stores a base64-encoded PNG for the server list MOTD.
func (s *State) SetIcon(b64 string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.iconBase64 = b64
}

// Icon returns the stored server icon.
func (s *State) Icon() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.iconBase64
}

// UpdatePlayers updates the player list from a Crafty or external source.
func (s *State) UpdatePlayers(count int, list []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.players = count
	if list != nil {
		s.playerList = list
	} else {
		s.playerList = []string{}
	}
}

// SetWorldSeed records the world seed shown on the dashboard.
func (s *State) SetWorldSeed(seed string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.worldSeed = seed
}

// SetMaxPlayers sets the cap shown in MOTD and dashboard.
func (s *State) SetMaxPlayers(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxPlayers = n
}

// Status returns a snapshot of the current state.
func (s *State) Status() ServerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ServerStatus{
		Phase:        s.phase,
		PhaseSince:   s.phaseSince,
		ServerOnline: s.serverOnline,
		Players:      s.players,
		MaxPlayers:   s.maxPlayers,
		PlayerList:   append([]string{}, s.playerList...),
		WorldSeed:    s.worldSeed,
		WorldName:    s.worldName,
	}
}

// --- Log buffer ---

// Logf appends a formatted timestamped message to the in-memory log.
func (s *State) Logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	s.mu.Lock()
	defer s.mu.Unlock()
	line := time.Now().Format("15:04:05") + "  " + msg
	fmt.Println(line)
	s.logs = append(s.logs, line)
	if len(s.logs) > s.logMaxLen {
		s.logs = s.logs[1:]
	}
}

// LogfServer appends a message to a specific server's log buffer.
func (s *State) LogfServer(hostname, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	s.mu.Lock()
	defer s.mu.Unlock()
	line := time.Now().Format("15:04:05") + "  " + msg
	// Also log globally.
	fmt.Println(line)
	s.logs = append(s.logs, line)
	if len(s.logs) > s.logMaxLen {
		s.logs = s.logs[1:]
	}
	// Per-server buffer.
	ss := s.ensureServer(hostname)
	if ss != nil {
		ss.logs = append(ss.logs, line)
		if len(ss.logs) > s.logMaxLen {
			ss.logs = ss.logs[1:]
		}
	}
}

// Logs returns a copy of the log buffer.
func (s *State) Logs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.logs))
	copy(out, s.logs)
	return out
}

// ServerLogs returns the log buffer for a specific server.
func (s *State) ServerLogs(hostname string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ss, ok := s.servers[hostname]; ok && ss.logs != nil {
		out := make([]string, len(ss.logs))
		copy(out, ss.logs)
		return out
	}
	return nil
}

// --- Localisation ---

// LangPack returns the translated messages for the configured locale.
func (s *State) LangPack() LangPack {
	s.mu.RLock()
	lang := s.lang
	s.mu.RUnlock()
	if lp, ok := locales[lang]; ok {
		return lp
	}
	return defaultLang
}

// SetLang changes the locale (en, pt, ...).
func (s *State) SetLang(lang string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lang = lang
}

// SetHealth stores the result of startup health checks.
func (s *State) SetHealth(h HealthResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health = h
}

// Health returns the stored health check result.
func (s *State) Health() HealthResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.health
}
