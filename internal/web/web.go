// Package web serves the mc-wake-proxy dashboard and API endpoints.
package web

import (
	"embed"
	"encoding/json"
	"net/http"

	"github.com/mefrraz/mc-wake-proxy/internal/proxy"
)

//go:embed templates/dashboard.html
var dashboardHTML embed.FS

// Start launches the HTTP dashboard server with server management API.
// reloadServers is called after adding/removing a server to update routing at runtime.
// stopServer/restartServer are Crafty action callbacks.
func Start(state *proxy.State, addr, configPath string, reloadServers func(string) error, stopServer, restartServer func(string) error, listServers func() ([]proxy.DiscoveredServer, error)) {
	mux := http.NewServeMux()

	// Dashboard page.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, _ := dashboardHTML.ReadFile("templates/dashboard.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	// API: current proxy status.
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		s := state.Status()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"phase":         s.Phase,
			"phase_since":   s.PhaseSince,
			"server_online": s.ServerOnline,
			"players":       s.Players,
			"max_players":   s.MaxPlayers,
			"player_list":   s.PlayerList,
			"world_seed":    s.WorldSeed,
			"world_name":    s.WorldName,
		})
	})

	// API: recent log lines.
	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state.Logs())
	})

	// API: health checks.
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state.Health())
	})

	// API: configured servers with status + add/remove.
	mux.HandleFunc("/api/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "GET":
			entries := state.ServerEntries()
			statuses := state.ServerStatuses()
			type serverInfo struct {
				Hostname string `json:"hostname"`
				Backend  string `json:"backend"`
				Online   bool   `json:"online"`
			}
			servers := make([]serverInfo, 0, len(entries))
			for _, e := range entries {
				servers = append(servers, serverInfo{
					Hostname: e.Hostname,
					Backend:  e.Backend,
					Online:   statuses[e.Hostname],
				})
			}
			json.NewEncoder(w).Encode(servers)

		case "POST":
			var entry proxy.ServerEntry
			if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON: " + err.Error()})
				return
			}
			if entry.Hostname == "" || entry.Backend == "" || entry.CraftyServerID == "" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "hostname, backend, and crafty_server_id are required"})
				return
			}
			if err := proxy.AddServerToFile(configPath, entry); err != nil {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if err := reloadServers(configPath); err != nil {
				state.Logf("WEB: reload after add failed: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

		case "DELETE":
			hostname := r.URL.Query().Get("hostname")
			if hostname == "" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "?hostname= is required"})
				return
			}
			if err := proxy.RemoveServerFromFile(configPath, hostname); err != nil {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if err := reloadServers(configPath); err != nil {
				state.Logf("WEB: reload after remove failed: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// API: server actions (stop, restart).
	mux.HandleFunc("/api/action/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		hostname := r.URL.Query().Get("hostname")
		action := r.URL.Path[len("/api/action/"):]
		if hostname == "" || action == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "?hostname= is required"})
			return
		}

		// Resolve Crafty server ID from config (case-insensitive).
		entries := state.ServerEntries()
		var craftyID string
		hLower := []byte(hostname)
		for i := range hLower {
			if hLower[i] >= 'A' && hLower[i] <= 'Z' {
				hLower[i] += 32
			}
		}
		for _, e := range entries {
			b := []byte(e.Hostname)
			for i := range b {
				if b[i] >= 'A' && b[i] <= 'Z' {
					b[i] += 32
				}
			}
			if string(hLower) == string(b) {
				craftyID = e.CraftyServerID
				break
			}
		}
		if craftyID == "" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "server not found"})
			return
		}

		var err error
		switch action {
		case "stop":
			err = stopServer(craftyID)
		case "restart":
			err = restartServer(craftyID)
		default:
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "unknown action"})
			return
		}

		if err != nil {
			state.Logf("WEB: action %s on %s failed: %v", action, hostname, err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		state.Logf("WEB: %s %s via Crafty", action, hostname)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// API: discover servers from Crafty.
	mux.HandleFunc("/api/crafty/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		servers, err := listServers()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(servers)
	})

	state.Logf("WEB: dashboard listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		state.Logf("WEB: server error: %v", err)
	}
}
