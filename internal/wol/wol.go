// Package wol implements Wake-on-LAN magic packet sending.
package wol

import (
	"fmt"
	"net"
	"strings"
)

// Sender sends Wake-on-LAN magic packets to a broadcast address.
type Sender interface {
	Send(mac string, broadcast string) error
}

// UDPSender is the default implementation that sends over UDP on port 9.
type UDPSender struct{}

// Send broadcasts a magic packet to wake the machine identified by mac.
// mac may use colons, hyphens, or no separator.
// broadcast is the IPv4 broadcast address, e.g. "192.168.1.255".
func (UDPSender) Send(mac string, broadcast string) error {
	mac = strings.NewReplacer(":", "", "-", "").Replace(mac)
	if len(mac) != 12 {
		return fmt.Errorf("wol: invalid MAC length (%d hex chars, expected 12)", len(mac))
	}

	var hw [6]byte
	_, err := fmt.Sscanf(mac, "%02x%02x%02x%02x%02x%02x",
		&hw[0], &hw[1], &hw[2], &hw[3], &hw[4], &hw[5])
	if err != nil {
		return fmt.Errorf("wol: cannot parse MAC %q: %w", mac, err)
	}

	// Build magic packet: 6 × 0xFF followed by 16 × MAC.
	packet := make([]byte, 6+16*6)
	for i := 0; i < 6; i++ {
		packet[i] = 0xFF
	}
	for i := 0; i < 16; i++ {
		copy(packet[6+i*6:6+(i+1)*6], hw[:])
	}

	addr := net.JoinHostPort(broadcast, "9")
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return fmt.Errorf("wol: UDP dial %s: %w", addr, err)
	}
	defer conn.Close()

	_, err = conn.Write(packet)
	if err != nil {
		return fmt.Errorf("wol: write to %s: %w", addr, err)
	}
	return nil
}
