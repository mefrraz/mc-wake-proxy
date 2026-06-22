// Package crafty implements a minimal Crafty Controller API client for server management.
package crafty

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client talks to the Crafty Controller REST API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a Crafty Controller API client.
//
// Parameters:
//   - host: Crafty hostname or IP (e.g. "192.168.1.131").
//   - port: Crafty web port (usually "8443" or "8000").
//   - scheme: "https" (default) or "http" for plain-text Crafty.
//   - token: API token obtained from the Crafty web interface.
//   - insecure: if true, skip TLS certificate verification.
func NewClient(host, port, scheme, token string, insecure bool) *Client {
	baseURL := fmt.Sprintf("%s://%s:%s/api/v2", scheme, host, port)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}
	if scheme == "http" {
		transport.TLSClientConfig = nil // no TLS for plain HTTP
	}
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
	}
}

// ServerInfo holds runtime information about a Minecraft server managed by Crafty.
type ServerInfo struct {
	ID        string `json:"id"`
	Running   bool   `json:"running"`
	Online    int    `json:"online"`
	Max       int    `json:"max"`
	Version   string `json:"version"`
	WorldName string `json:"world_name"`
	Desc      string `json:"desc"`
	Icon      string `json:"icon"`
}

// ServerConfigInfo holds static server configuration from Crafty.
type ServerConfigInfo struct {
	ID       string `json:"server_id"`
	Name     string `json:"server_name"`
	IP       string `json:"server_ip"`
	Port     int    `json:"server_port"`
}

// GetServerStatus returns runtime status for a specific server by its Crafty server_id.
func (c *Client) GetServerStatus(serverID string) (*ServerInfo, error) {
	var servers []ServerInfo
	if err := c.get("/servers/status/", &servers); err != nil {
		return nil, err
	}
	for _, s := range servers {
		if s.ID == serverID {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("crafty: server %q not found in status response (check show_status is enabled)", serverID)
}

// StartServer starts a Minecraft server by its Crafty server_id.
func (c *Client) StartServer(serverID string) error {
	path := fmt.Sprintf("/servers/%s/action/start_server", serverID)
	return c.post(path, nil)
}

// StopServer stops a Minecraft server by its Crafty server_id.
func (c *Client) StopServer(serverID string) error {
	path := fmt.Sprintf("/servers/%s/action/stop_server", serverID)
	return c.post(path, nil)
}

// RestartServer restarts a Minecraft server by its Crafty server_id.
func (c *Client) RestartServer(serverID string) error {
	path := fmt.Sprintf("/servers/%s/action/restart_server", serverID)
	return c.post(path, nil)
}

// ListServers returns all servers visible via the status endpoint.
func (c *Client) ListServers() ([]ServerInfo, error) {
	var servers []ServerInfo
	if err := c.get("/servers/status/", &servers); err != nil {
		return nil, err
	}
	return servers, nil
}

// GetServerConfig returns the static configuration for a server.
func (c *Client) GetServerConfig(serverID string) (*ServerConfigInfo, error) {
	var cfg ServerConfigInfo
	if err := c.get("/servers/"+serverID, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// get performs a GET request and decodes the Crafty JSON envelope.
func (c *Client) get(path string, v interface{}) error {
	return c.do("GET", path, nil, v)
}

// post performs a POST request.
func (c *Client) post(path string, body []byte) error {
	return c.do("POST", path, body, nil)
}

// do is the low-level HTTP executor that handles Crafty's {"status":"ok","data":...} envelope.
func (c *Client) do(method, path string, body []byte, v interface{}) error {
	url := c.baseURL + path
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("crafty: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("crafty: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("crafty: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("crafty: %s %s returned HTTP %d: %s", method, path, resp.StatusCode, string(respBytes))
	}

	// Crafty wraps responses in {"status":"ok","data":...}
	var envelope struct {
		Status string          `json:"status"`
		Error  string          `json:"error"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &envelope); err != nil {
		return fmt.Errorf("crafty: decode envelope: %w", err)
	}
	if envelope.Status != "ok" && envelope.Error != "" {
		return fmt.Errorf("crafty: API error: %s", envelope.Error)
	}

	if v != nil && envelope.Data != nil {
		if err := json.Unmarshal(envelope.Data, v); err != nil {
			return fmt.Errorf("crafty: decode data: %w (raw: %s)", err, string(envelope.Data))
		}
	}
	return nil
}

// Compile-time interface check.
var _ ServerManager = (*Client)(nil)

// ServerManager is the interface for Minecraft server management via Crafty.
type ServerManager interface {
	GetServerStatus(serverID string) (*ServerInfo, error)
	StartServer(serverID string) error
	StopServer(serverID string) error
	RestartServer(serverID string) error
}
