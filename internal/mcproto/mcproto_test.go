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
	jsonStr := `{"version":{"name":"Proxy","protocol":758},"players":{"max":10,"online":3},"description":{"text":"Hello"}}`
	pkt := StatusResponse(jsonStr)

	// Verify packet structure
	r := bytes.NewReader(pkt)
	_, err := ReadVarInt(r)
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
	decodedJSON, err := ReadString(r)
	if err != nil {
		t.Fatal(err)
	}
	if decodedJSON == "" {
		t.Fatal("expected non-empty JSON")
	}
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

func TestReadPacketRaw(t *testing.T) {
	// Build a handshake packet body.
	var body []byte
	body = append(body, WriteVarInt(758)...)
	body = append(body, WriteString("test.example.com")...)
	body = append(body, 0x63, 0xDD) // port 25565
	body = append(body, WriteVarInt(2)...) // next_state=login

	hsPkt := MakePacket(0x00, body)

	// Read it back.
	raw, err := ReadPacketRaw(bytes.NewReader(hsPkt))
	if err != nil {
		t.Fatalf("ReadPacketRaw: %v", err)
	}

	// Verify the raw bytes roundtrip.
	if len(raw) != len(hsPkt) {
		t.Fatalf("roundtrip length mismatch: %d vs %d", len(raw), len(hsPkt))
	}
	for i := range raw {
		if raw[i] != hsPkt[i] {
			t.Fatalf("roundtrip byte %d: %d vs %d", i, raw[i], hsPkt[i])
		}
	}
}

func TestParseHandshakeFromRaw(t *testing.T) {
	// Build a handshake body (without length prefix).
	var body []byte
	body = append(body, WriteVarInt(0x00)...) // packet ID
	body = append(body, WriteVarInt(758)...)
	body = append(body, WriteString("survival.mc.example.com")...)
	body = append(body, 0x63, 0xDD)           // port 25565
	body = append(body, WriteVarInt(2)...)    // next_state=login

	hs, err := ParseHandshake(body)
	if err != nil {
		t.Fatalf("ParseHandshake: %v", err)
	}
	if hs.ProtocolVersion != 758 {
		t.Fatalf("expected proto 758, got %d", hs.ProtocolVersion)
	}
	if hs.ServerAddress != "survival.mc.example.com" {
		t.Fatalf("expected hostname, got %q", hs.ServerAddress)
	}
	if hs.ServerPort != 25565 {
		t.Fatalf("expected port 25565, got %d", hs.ServerPort)
	}
	if hs.NextState != 2 {
		t.Fatalf("expected nextState 2, got %d", hs.NextState)
	}
}

func TestReadStringFromBytes(t *testing.T) {
	encoded := WriteString("hello proxy")
	s, err := ReadStringFromBytes(encoded)
	if err != nil {
		t.Fatalf("ReadStringFromBytes: %v", err)
	}
	if s != "hello proxy" {
		t.Fatalf("expected 'hello proxy', got %q", s)
	}
}

func TestStripPacketPrefix(t *testing.T) {
	// Build a handshake packet with a 1-byte length prefix.
	var body []byte
	body = append(body, WriteVarInt(758)...)
	body = append(body, WriteString("test.example.com")...)
	body = append(body, 0x63, 0xDD)
	body = append(body, WriteVarInt(2)...)
	pkt := MakePacket(0x00, body)

	stripped, err := StripPacketPrefix(pkt)
	if err != nil {
		t.Fatalf("StripPacketPrefix: %v", err)
	}
	if len(stripped) != len(body)+1 { // body + packet ID byte
		t.Fatalf("expected %d bytes, got %d", len(body)+1, len(stripped))
	}
}

func TestParseLoginStart(t *testing.T) {
	// Build a Login Start body: packet ID 0x00 + "Steve"
	var body []byte
	body = append(body, WriteVarInt(0x00)...) // packet ID
	body = append(body, WriteString("Steve")...)

	name, err := ParseLoginStart(body)
	if err != nil {
		t.Fatalf("ParseLoginStart: %v", err)
	}
	if name != "Steve" {
		t.Fatalf("expected 'Steve', got %q", name)
	}
}

func TestParseLoginStartBadPacketID(t *testing.T) {
	var body []byte
	body = append(body, WriteVarInt(0x01)...) // wrong packet ID
	body = append(body, WriteString("Steve")...)

	_, err := ParseLoginStart(body)
	if err == nil {
		t.Fatal("expected error for bad packet ID")
	}
}
