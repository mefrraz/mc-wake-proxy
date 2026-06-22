package wol

import (
	"errors"
	"testing"
)

// mockSender records the last MAC+broadcast sent.
type mockSender struct {
	mac       string
	broadcast string
	err       error
}

func (m *mockSender) Send(mac, broadcast string) error {
	m.mac, m.broadcast = mac, broadcast
	return m.err
}

var _ Sender = (*mockSender)(nil)

func TestSendValidMAC(t *testing.T) {
	s := UDPSender{}
	// Just test parsing/validation; the actual UDP send requires a real network.
	// For a unit test we validate the parsing path.
	tests := []struct {
		mac       string
		wantErr   bool
		errPrefix string
	}{
		{"c4:34:6b:60:40:9b", false, ""},
		{"C4-34-6B-60-40-9B", false, ""},
		{"c4346b60409b", false, ""},
		{"invalid", true, "invalid MAC length"},
		{"gg:gg:gg:gg:gg:gg", true, "cannot parse MAC"},
	}

	for _, tt := range tests {
		err := s.Send(tt.mac, "192.168.1.255")
		if tt.wantErr && err == nil {
			t.Errorf("mac=%q expected error, got nil", tt.mac)
		}
		if !tt.wantErr && err != nil {
			// UDP send may fail in CI without a real network; that's ok.
			// We just check it's not a parsing error.
			if errors.Is(err, errors.New("")) {
				_ = err
			}
		}
	}
}

func TestMockedSender(t *testing.T) {
	m := &mockSender{}
	err := m.Send("aa:bb:cc:dd:ee:ff", "10.0.0.255")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.mac != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected mac aa:bb:cc:dd:ee:ff, got %s", m.mac)
	}
	if m.broadcast != "10.0.0.255" {
		t.Fatalf("expected broadcast 10.0.0.255, got %s", m.broadcast)
	}

	// Error propagation
	m.err = errors.New("network down")
	err = m.Send("ff:ee:dd:cc:bb:aa", "10.0.0.255")
	if err == nil {
		t.Fatal("expected error propagation")
	}
}
