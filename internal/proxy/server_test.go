package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadServersValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.yml")
	data := `
servers:
  - hostname: survival.mc.example.com
    backend: 192.168.1.131:25566
    crafty_server_id: abc-123
  - hostname: creative.mc.example.com
    backend: 192.168.1.131:25567
    crafty_server_id: def-456
`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	sc, err := LoadServers(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sc.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(sc.Servers))
	}
}

func TestLoadServersNotFound(t *testing.T) {
	sc, err := LoadServers("/nonexistent/servers.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc != nil {
		t.Fatal("expected nil for missing file")
	}
}

func TestLoadServersInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.yml")

	os.WriteFile(path, []byte("garbage: [[["), 0644)
	_, err := LoadServers(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadServersMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.yml")

	os.WriteFile(path, []byte("servers:\n  - hostname: test\n"), 0644)
	_, err := LoadServers(path)
	if err == nil {
		t.Fatal("expected missing backend error")
	}
}

func TestServerLookup(t *testing.T) {
	sc := &ServersConfig{
		Servers: []ServerEntry{
			{Hostname: "survival.mc.example.com", Backend: "1.2.3.4:25566", CraftyServerID: "abc"},
			{Hostname: "creative.mc.example.com", Backend: "1.2.3.4:25567", CraftyServerID: "def"},
		},
	}

	// Exact match
	s := sc.Lookup("survival.mc.example.com")
	if s == nil || s.CraftyServerID != "abc" {
		t.Fatal("expected survival match")
	}

	// Case-insensitive
	s = sc.Lookup("SURVIVAL.MC.EXAMPLE.COM")
	if s == nil || s.CraftyServerID != "abc" {
		t.Fatal("expected case-insensitive match")
	}

	// Not found
	s = sc.Lookup("unknown.example.com")
	if s != nil {
		t.Fatal("expected nil for unknown hostname")
	}
}
