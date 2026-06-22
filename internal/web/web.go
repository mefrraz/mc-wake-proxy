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

// Start launches the HTTP dashboard server.  It blocks until the server fails.
func Start(state *proxy.State, addr string) {
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

	state.Logf("WEB: dashboard listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		state.Logf("WEB: server error: %v", err)
	}
}
