// Package proxmox implements a minimal Proxmox VE API client for LXC management.
package proxmox

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LXCManager is the interface for LXC operations needed by the proxy.
type LXCManager interface {
	GetLXCStatus(node, lxcID string) (*LXCStatus, error)
	StartLXC(node, lxcID string) (string, error)
}

// Client talks to the Proxmox VE REST API using an API token.
type Client struct {
	baseURL       string
	tokenID       string
	tokenSecret   string
	insecure      bool
	httpClient    *http.Client
}

// NewClient creates a Proxmox API client.
//
// Parameters:
//   - host: Proxmox hostname or IP (e.g. "192.168.1.10").
//   - port: API port (usually "8006").
//   - tokenID: API token ID in the form "user@realm!tokenname".
//   - tokenSecret: the secret UUID associated with the token.
//   - insecure: if true, skip TLS certificate verification (default for self-signed certs).
func NewClient(host, port, tokenID, tokenSecret string, insecure bool) *Client {
	baseURL := fmt.Sprintf("https://%s:%s/api2/json", host, port)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}
	return &Client{
		baseURL:     baseURL,
		tokenID:     tokenID,
		tokenSecret: tokenSecret,
		insecure:    insecure,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
	}
}

// do executes an HTTP request and decodes the JSON response into v.
// The Proxmox API wraps everything in {"data": ...}.
func (c *Client) do(method, path string, body []byte, v interface{}) error {
	url := c.baseURL + path
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("proxmox: build request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken %s=%s", c.tokenID, c.tokenSecret))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("proxmox: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("proxmox: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxmox: %s %s returned HTTP %d: %s", method, path, resp.StatusCode, string(respBytes))
	}

	// Proxmox wraps responses in {"data": ...}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &envelope); err != nil {
		return fmt.Errorf("proxmox: decode envelope: %w", err)
	}

	if v != nil {
		if err := json.Unmarshal(envelope.Data, v); err != nil {
			return fmt.Errorf("proxmox: decode data: %w (raw: %s)", err, string(envelope.Data))
		}
	}
	return nil
}

// LXCStatus holds the current status of an LXC container.
type LXCStatus struct {
	Status string `json:"status"` // "running" or "stopped"
}

// GetLXCStatus returns the current status of the LXC container.
func (c *Client) GetLXCStatus(node, lxcID string) (*LXCStatus, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%s/status/current", node, lxcID)
	var s LXCStatus
	if err := c.do("GET", path, nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// StartLXC starts the LXC container. Returns the UPID on success.
func (c *Client) StartLXC(node, lxcID string) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%s/status/start", node, lxcID)
	var upid string
	if err := c.do("POST", path, nil, &upid); err != nil {
		return "", err
	}
	return upid, nil
}
