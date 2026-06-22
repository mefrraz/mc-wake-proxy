package crafty

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetServerStatusRunning(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token-123" {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/api/v2/servers/status/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"data": []map[string]interface{}{
				{
					"id":      "abc-123",
					"running": true,
					"online":  2,
					"max":     20,
				},
				{
					"id":      "def-456",
					"running": false,
					"online":  0,
					"max":     10,
				},
			},
		})
	}))
	defer srv.Close()

	c := &Client{
		baseURL:    srv.URL + "/api/v2",
		token:      "test-token-123",
		httpClient: srv.Client(),
	}

	info, err := c.GetServerStatus("abc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !info.Running {
		t.Fatal("expected running=true")
	}
	if info.Online != 2 {
		t.Fatalf("expected 2 online, got %d", info.Online)
	}

	info2, err := c.GetServerStatus("def-456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info2.Running {
		t.Fatal("expected running=false")
	}
}

func TestGetServerStatusNotFound(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"data":   []map[string]interface{}{},
		})
	}))
	defer srv.Close()

	c := &Client{
		baseURL:    srv.URL + "/api/v2",
		token:      "test",
		httpClient: srv.Client(),
	}

	_, err := c.GetServerStatus("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing server")
	}
}

func TestStartServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v2/servers/abc-123/action/start_server" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer srv.Close()

	c := &Client{
		baseURL:    srv.URL + "/api/v2",
		token:      "test",
		httpClient: srv.Client(),
	}

	err := c.StartServer("abc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := &Client{
		baseURL:    srv.URL + "/api/v2",
		token:      "bad-token",
		httpClient: srv.Client(),
	}

	_, err := c.GetServerStatus("abc")
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  "NOT_AUTHORIZED",
		})
	}))
	defer srv.Close()

	c := &Client{
		baseURL:    srv.URL + "/api/v2",
		token:      "bad",
		httpClient: srv.Client(),
	}

	err := c.StartServer("abc")
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}
