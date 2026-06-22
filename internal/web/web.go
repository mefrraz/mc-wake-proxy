package web

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mefrraz/mc-wake-proxy/internal/proxy"
)

//go:embed templates/dashboard.html
var dashboardHTML embed.FS

// Start launches the HTTP dashboard server.
func Start(state *proxy.State, addr, configPath, password string, reloadServers func(string) error, stopServer, restartServer, startServer func(string) error, sendCommand func(string, string) error, listServers func() ([]proxy.DiscoveredServer, error)) {
	mux := http.NewServeMux()

	// Session token from password hash.
	var sessionToken string
	if password != "" {
		h := sha256.Sum256([]byte(password + "mc-wake-proxy-salt"))
		sessionToken = fmt.Sprintf("%x", h)
	}

	// Auth check helper.
	checkAuth := func(w http.ResponseWriter, r *http.Request) bool {
		if sessionToken == "" {
			return true
		}
		cookie, err := r.Cookie("mc_session")
		if err == nil && cookie.Value == sessionToken {
			return true
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return false
	}

	// Login handler.
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if password == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if r.Method == "POST" {
			r.ParseForm()
			if r.FormValue("password") == password {
				http.SetCookie(w, &http.Cookie{Name: "mc_session", Value: sessionToken, Path: "/", MaxAge: 86400 * 30, HttpOnly: true})
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<html><body style="background:#0f1419;color:#e2e6ed;font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;"><form method=post><h2>mc-wake-proxy</h2><input name=password type=password placeholder=Password autofocus style="padding:8px;border-radius:6px;border:1px solid #2a3140;background:#1a1f2b;color:#e2e6ed;font-size:14px;"><button style="margin-left:8px;padding:8px 16px;border-radius:6px;border:none;background:#4f8cff;color:#fff;font-weight:600;cursor:pointer;">Login</button><p style="color:#e74c3c;font-size:12px;">Wrong password</p></form></body></html>`))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><body style="background:#0f1419;color:#e2e6ed;font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;"><form method=post><h2>mc-wake-proxy</h2><input name=password type=password placeholder=Password autofocus style="padding:8px;border-radius:6px;border:1px solid #2a3140;background:#1a1f2b;color:#e2e6ed;font-size:14px;"><button style="margin-left:8px;padding:8px 16px;border-radius:6px;border:none;background:#4f8cff;color:#fff;font-weight:600;cursor:pointer;">Login</button></form></body></html>`))
	})

	// Root — redirect to login if needed.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if sessionToken != "" {
			cookie, err := r.Cookie("mc_session")
			if err != nil || cookie.Value != sessionToken {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
		}
		data, _ := dashboardHTML.ReadFile("templates/dashboard.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	// API handlers (all wrapped with auth).
	api := func(path string, h http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !checkAuth(w, r) { return }
			h(w, r)
		})
	}

	api("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		s := state.Status()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"phase": s.Phase, "phase_since": s.PhaseSince, "server_online": s.ServerOnline,
			"players": s.Players, "max_players": s.MaxPlayers, "player_list": s.PlayerList,
			"world_seed": s.WorldSeed, "world_name": s.WorldName,
			"crafty_node": state.CraftyNode(),
		})
	})

	api("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		hostname := r.URL.Query().Get("hostname")
		var logs []string
		if hostname != "" {
			logs = state.ServerLogs(hostname)
			if logs == nil {
				for _, l := range state.Logs() {
					if strings.Contains(l, hostname) { logs = append(logs, l) }
				}
			}
			if logs == nil { logs = []string{} }
		} else {
			logs = state.Logs()
		}
		json.NewEncoder(w).Encode(logs)
	})

	api("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state.Health())
	})

	api("/api/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "GET":
			entries := state.ServerEntries()
			statuses := state.ServerStatuses()
			type si struct {
				Hostname       string `json:"hostname"`
				Backend        string `json:"backend"`
				CraftyServerID string `json:"crafty_server_id"`
				Online         bool   `json:"online"`
			}
			out := make([]si, 0, len(entries))
			for _, e := range entries {
				out = append(out, si{e.Hostname, e.Backend, e.CraftyServerID, statuses[e.Hostname]})
			}
			json.NewEncoder(w).Encode(out)
		case "POST":
			var e proxy.ServerEntry
			if err := json.NewDecoder(r.Body).Decode(&e); err != nil { w.WriteHeader(400); return }
			if e.Hostname == "" || e.Backend == "" || e.CraftyServerID == "" { w.WriteHeader(400); return }
			if err := proxy.AddServerToFile(configPath, e); err != nil { w.WriteHeader(409); json.NewEncoder(w).Encode(map[string]string{"error":err.Error()}); return }
			reloadServers(configPath)
			w.WriteHeader(201); json.NewEncoder(w).Encode(map[string]string{"status":"ok"})
		case "DELETE":
			hostname := r.URL.Query().Get("hostname")
			if hostname == "" { w.WriteHeader(400); return }
			if err := proxy.RemoveServerFromFile(configPath, hostname); err != nil { w.WriteHeader(404); json.NewEncoder(w).Encode(map[string]string{"error":err.Error()}); return }
			reloadServers(configPath)
			json.NewEncoder(w).Encode(map[string]string{"status":"ok"})
		default:
			w.WriteHeader(405)
		}
	})

	api("/api/action/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" { w.WriteHeader(405); return }
		hostname := r.URL.Query().Get("hostname")
		action := r.URL.Path[len("/api/action/"):]
		if hostname == "" || action == "" { w.WriteHeader(400); return }
		entries := state.ServerEntries()
		var craftyID string
		for _, e := range entries {
			if strings.EqualFold(e.Hostname, hostname) { craftyID = e.CraftyServerID; break }
		}
		if craftyID == "" { w.WriteHeader(404); return }
		var err error
		switch action {
		case "start": err = startServer(craftyID)
		case "stop":
			state.SetPhaseForServer(hostname, proxy.PhaseStopping)
			state.Logf("WEB: stopping %s", hostname)
			err = stopServer(craftyID)
		case "restart": err = restartServer(craftyID)
		default: w.WriteHeader(400); return
		}
		if err != nil { w.WriteHeader(500); json.NewEncoder(w).Encode(map[string]string{"error":err.Error()}); return }
		json.NewEncoder(w).Encode(map[string]string{"status":"ok"})
	})

	api("/api/console", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" { w.WriteHeader(405); return }
		hostname := r.URL.Query().Get("hostname")
		cmd := r.URL.Query().Get("cmd")
		if hostname == "" || cmd == "" { w.WriteHeader(400); return }
		entries := state.ServerEntries()
		var craftyID string
		for _, e := range entries {
			if strings.EqualFold(e.Hostname, hostname) { craftyID = e.CraftyServerID; break }
		}
		if craftyID == "" { w.WriteHeader(404); return }
		cmd = strings.TrimPrefix(cmd, "/")
		if err := sendCommand(craftyID, cmd); err != nil { w.WriteHeader(500); json.NewEncoder(w).Encode(map[string]string{"error":err.Error()}); return }
		state.Logf("CONSOLE: %s > %s", hostname, cmd)
		json.NewEncoder(w).Encode(map[string]string{"status":"ok"})
	})

	api("/api/crafty/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		servers, err := listServers()
		if err != nil { w.WriteHeader(500); json.NewEncoder(w).Encode(map[string]string{"error":err.Error()}); return }
		json.NewEncoder(w).Encode(servers)
	})

	// Serve logo.
	mux.HandleFunc("/logo.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, "docs/logo.png")
	})

	state.Logf("WEB: dashboard listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		state.Logf("WEB: server error: %v", err)
	}
}
