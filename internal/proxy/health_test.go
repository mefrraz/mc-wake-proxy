package proxy

import (
	"errors"
	"testing"

	"github.com/mefrraz/mc-wake-proxy/internal/crafty"
	"github.com/mefrraz/mc-wake-proxy/internal/proxmox"
)

type mockPM struct{ status *proxmox.LXCStatus; err error }

func (m *mockPM) GetLXCStatus(node, lxcID string) (*proxmox.LXCStatus, error) { return m.status, m.err }
func (m *mockPM) StartLXC(node, lxcID string) (string, error)                   { return "UPID:ok", m.err }
func (m *mockPM) GetLXCResources(node, lxcID string) (*proxmox.LXCResources, error) { return nil, nil }

type mockCM struct{ info *crafty.ServerInfo; err error }

func (m *mockCM) GetServerStatus(serverID string) (*crafty.ServerInfo, error) { return m.info, m.err }
func (m *mockCM) StartServer(serverID string) error                            { return m.err }
func (m *mockCM) StopServer(serverID string) error                             { return m.err }
func (m *mockCM) RestartServer(serverID string) error                          { return m.err }

func TestHealthCheckAllOK(t *testing.T) {
	cfg := &Config{
		ProxmoxNode:        "proserver",
		ProxmoxLXCID:       "102",
		CraftyServerID:     "0106e032-b04d-4a5f-9660-baf7c6a161a3",
		WOLMAC:             "c4:34:6b:60:40:9b",
		WOLBroadcast:       "192.168.1.255",
		BackendTarget:      "192.168.1.131:25565",
	}
	pm := &mockPM{status: &proxmox.LXCStatus{Status: "running"}}
	cm := &mockCM{info: &crafty.ServerInfo{Running: true, Online: 2}}

	result := RunHealthChecks(cfg, pm, cm)
	// Backend check does real TCP dial — may fail in test environment.
	// At minimum, Proxmox + Crafty + WOL must be OK.
	if len(result.Checks) != 4 {
		t.Fatalf("expected 4 checks, got %d", len(result.Checks))
	}
	if !result.Checks[0].OK || !result.Checks[1].OK || !result.Checks[2].OK {
		t.Fatalf("expected Proxmox+Crafty+WOL OK, got: %+v", result.Checks)
	}
}

func TestHealthCheckProxmoxFail(t *testing.T) {
	cfg := &Config{
		ProxmoxNode:        "wrong-node",
		ProxmoxLXCID:       "102",
		CraftyServerID:     "0106e032-b04d-4a5f-9660-baf7c6a161a3",
		WOLMAC:             "c4:34:6b:60:40:9b",
		WOLBroadcast:       "192.168.1.255",
		BackendTarget:      "192.168.1.131:25565",
	}
	pm := &mockPM{err: errors.New(`proxmox: GET /nodes/wrong-node/lxc/102/status/current returned HTTP 500: {"data":null,"message":"hostname lookup 'wrong-node' failed - failed to get address info for: wrong-node: Name or service not known\n"}`)}
	cm := &mockCM{info: &crafty.ServerInfo{Running: true}} // Crafty is fine, only Proxmox fails

	result := RunHealthChecks(cfg, pm, cm)
	if result.AllOK {
		t.Fatal("expected AllOK=false when Proxmox fails")
	}
	if result.Checks[0].OK {
		t.Fatal("expected Proxmox check to fail")
	}
	if !result.Checks[1].OK {
		t.Fatal("expected Crafty check to pass (mock returns ok)")
	}
}

func TestHealthCheckWOLInvalidMAC(t *testing.T) {
	cfg := &Config{
		ProxmoxNode:        "proserver",
		ProxmoxLXCID:       "102",
		CraftyServerID:     "0106e032",
		WOLMAC:             "bad-mac",
		WOLBroadcast:       "192.168.1.255",
		BackendTarget:      "192.168.1.131:25565",
	}
	pm := &mockPM{status: &proxmox.LXCStatus{Status: "running"}}
	cm := &mockCM{info: &crafty.ServerInfo{Running: true}}

	result := RunHealthChecks(cfg, pm, cm)
	if result.Checks[2].OK {
		t.Fatal("expected WOL check to fail with invalid MAC")
	}
}

func TestDiagnoseProxmoxError(t *testing.T) {
	tests := []struct {
		errMsg    string
		wantEmpty bool
		tag       string
	}{
		{"Permission check failed (/vms/102, VM.Audit)", false, "perms"},
		{"hostname lookup 'pve' failed", false, "lookup"},
		{"connection refused", false, "conn refused"},
		{"x509: certificate signed by unknown authority", false, "tls"},
		{"unexpected EOF", true, "unknown"},
	}
	for _, tt := range tests {
		err := errors.New(tt.errMsg)
		hint := diagnoseProxmoxError(err)
		if tt.wantEmpty && hint != "" {
			t.Errorf("[%s] expected empty hint, got: %s", tt.tag, hint)
		}
		if !tt.wantEmpty && hint == "" {
			t.Errorf("[%s] expected hint, got empty", tt.tag)
		}
	}
}
