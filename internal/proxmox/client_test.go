package proxmox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetLXCStatusRunning(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		if r.Header.Get("Authorization") != "PVEAPIToken root@pam!test=abc123" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/api2/json/nodes/pve/lxc/100/status/current" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"status": "running",
			},
		})
	}))
	defer srv.Close()

	// Construct client pointing at the test server.
	c := &Client{
		baseURL:     srv.URL + "/api2/json",
		tokenID:     "root@pam!test",
		tokenSecret: "abc123",
		httpClient:  srv.Client(),
	}

	status, err := c.GetLXCStatus("pve", "100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != "running" {
		t.Fatalf("expected 'running', got %q", status.Status)
	}
}

func TestGetLXCStatusStopped(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"status": "stopped",
			},
		})
	}))
	defer srv.Close()

	c := &Client{
		baseURL:     srv.URL + "/api2/json",
		tokenID:     "root@pam!test",
		tokenSecret: "abc123",
		httpClient:  srv.Client(),
	}

	status, err := c.GetLXCStatus("pve", "101")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != "stopped" {
		t.Fatalf("expected 'stopped', got %q", status.Status)
	}
}

func TestStartLXC(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": "UPID:pve:00000001:00000001:00000001:cmd:root@pam:",
		})
	}))
	defer srv.Close()

	c := &Client{
		baseURL:     srv.URL + "/api2/json",
		tokenID:     "root@pam!test",
		tokenSecret: "abc123",
		httpClient:  srv.Client(),
	}

	upid, err := c.StartLXC("pve", "100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upid == "" {
		t.Fatal("expected non-empty UPID")
	}
}

func TestHTTPError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := &Client{
		baseURL:     srv.URL + "/api2/json",
		tokenID:     "bad",
		tokenSecret: "token",
		httpClient:  srv.Client(),
	}

	_, err := c.GetLXCStatus("pve", "100")
	if err == nil {
		t.Fatal("expected error for 403")
	}
}
