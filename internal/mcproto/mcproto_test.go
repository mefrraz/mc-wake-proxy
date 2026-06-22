package mcproto

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestVarIntRoundtrip(t *testing.T) {
	tests := []int{0, 1, 127, 128, 255, 65535, 2097151, 2147483647}
	for _, v := range tests {
		encoded := WriteVarInt(v)
		decoded, err := ReadVarInt(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("ReadVarInt(%d): unexpected error: %v", v, err)
		}
		if decoded != v {
			t.Fatalf("VarInt roundtrip: %d → %d", v, decoded)
		}
	}
}

func TestVarIntOverflow(t *testing.T) {
	// Build a VarInt that overflows 5 bytes.
	var buf bytes.Buffer
	for i := 0; i < 5; i++ {
		buf.WriteByte(0xFF)
	}
	buf.WriteByte(0x7F) // 6th byte has MSB unset, so it will overflow the 32-bit shift
	_, err := ReadVarInt(&buf)
	if err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestReadString(t *testing.T) {
	s := "hello world"
	encoded := WriteString(s)
	decoded, err := ReadString(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decoded != s {
		t.Fatalf("expected %q, got %q", s, decoded)
	}
}

func TestReadHandshake(t *testing.T) {
	// Build a handshake packet body (after length + packet ID 0x00):
	// protocol_version=758, server_address="example.com", port=25565, next_state=1
	var buf bytes.Buffer
	buf.Write(WriteVarInt(758))
	buf.Write(WriteString("example.com"))
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, 25565)
	buf.Write(portBytes)
	buf.Write(WriteVarInt(1))

	h, err := ReadHandshake(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.ProtocolVersion != 758 {
		t.Fatalf("expected protocol 758, got %d", h.ProtocolVersion)
	}
	if h.ServerAddress != "example.com" {
		t.Fatalf("expected 'example.com', got %q", h.ServerAddress)
	}
	if h.ServerPort != 25565 {
		t.Fatalf("expected port 25565, got %d", h.ServerPort)
	}
	if h.NextState != 1 {
		t.Fatalf("expected next_state 1, got %d", h.NextState)
	}
}

func TestReadHandshakeMultiServer(t *testing.T) {
	// Simulate a player connecting to survival.minecraft.example.com
	var buf bytes.Buffer
	buf.Write(WriteVarInt(760))
	buf.Write(WriteString("survival.minecraft.example.com"))
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, 25565)
	buf.Write(portBytes)
	buf.Write(WriteVarInt(2)) // next_state = login

	h, err := ReadHandshake(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.ServerAddress != "survival.minecraft.example.com" {
		t.Fatalf("expected hostname, got %q", h.ServerAddress)
	}
	if h.NextState != 2 {
		t.Fatalf("expected next_state 2 (login), got %d", h.NextState)
	}
}

func TestStatusResponse(t *testing.T) {
	tmpl := `{"version":{"name":"Proxy","protocol":%d},"players":{"max":%d,"online":%d},"description":{"text":"Hello"}}`
	pkt := StatusResponse(tmpl, 758, 10, 3)

	// Verify packet structure
	r := bytes.NewReader(pkt)
	length, err := ReadVarInt(r)
	if err != nil {
		t.Fatal(err)
	}
	pktID, err := ReadVarInt(r)
	if err != nil {
		t.Fatal(err)
	}
	if pktID != 0x00 {
		t.Fatalf("expected packet ID 0x00, got 0x%02x", pktID)
	}
	jsonStr, err := ReadString(r)
	if err != nil {
		t.Fatal(err)
	}
	if jsonStr == "" {
		t.Fatal("expected non-empty JSON")
	}
	_ = length // used
}

func TestPongResponse(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	pkt := PongResponse(payload)

	r := bytes.NewReader(pkt)
	ReadVarInt(r) // length
	id, _ := ReadVarInt(r)
	if id != 0x01 {
		t.Fatalf("expected pong packet ID 0x01, got 0x%02x", id)
	}
	body := make([]byte, 8)
	_, err := r.Read(body)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range body {
		if b != payload[i] {
			t.Fatalf("pong body byte %d: expected %d, got %d", i, payload[i], b)
		}
	}
}

func TestLoginDisconnect(t *testing.T) {
	pkt := LoginDisconnect(`{"text":"Server is waking up"}`)
	r := bytes.NewReader(pkt)
	ReadVarInt(r) // length
	id, _ := ReadVarInt(r)
	if id != 0x00 {
		t.Fatalf("expected disconnect packet ID 0x00, got 0x%02x", id)
	}
	chat, err := ReadString(r)
	if err != nil {
		t.Fatal(err)
	}
	if chat != `{"text":"Server is waking up"}` {
		t.Fatalf("unexpected chat: %s", chat)
	}
}
