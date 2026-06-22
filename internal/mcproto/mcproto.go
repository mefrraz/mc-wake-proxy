// Package mcproto implements minimal Minecraft protocol support for handshake,
// status response, and login disconnect.
package mcproto

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// ReadVarInt reads a Minecraft VarInt from the reader.
func ReadVarInt(r io.Reader) (int, error) {
	var num int
	var shift uint
	for {
		b := make([]byte, 1)
		if _, err := io.ReadFull(r, b); err != nil {
			return 0, err
		}
		num |= int(b[0]&0x7F) << shift
		if (b[0] & 0x80) == 0 {
			break
		}
		shift += 7
		if shift >= 32 {
			return 0, fmt.Errorf("mcproto: VarInt overflow")
		}
	}
	return num, nil
}

// WriteVarInt encodes a value as a Minecraft VarInt.
func WriteVarInt(val int) []byte {
	var buf []byte
	for {
		b := byte(val & 0x7F)
		val >>= 7
		if val != 0 {
			b |= 0x80
		}
		buf = append(buf, b)
		if val == 0 {
			break
		}
	}
	return buf
}

// ReadString reads a Minecraft string (length-prefixed by a VarInt).
func ReadString(r io.Reader) (string, error) {
	l, err := ReadVarInt(r)
	if err != nil {
		return "", err
	}
	buf := make([]byte, l)
	_, err = io.ReadFull(r, buf)
	return string(buf), err
}

// ReadStringFromBytes reads a Minecraft string from a byte slice, returning the
// decoded string and the remaining bytes.  Useful for parsing raw packets.
func ReadStringFromBytes(b []byte) (string, error) {
	r := bytes.NewReader(b)
	return ReadString(r)
}

// WriteString encodes a string as length-prefixed VarInt + UTF-8 bytes.
func WriteString(s string) []byte {
	b := []byte(s)
	return append(WriteVarInt(len(b)), b...)
}

// MakePacket builds a complete Minecraft packet given a packet ID and payload.
func MakePacket(id int, data []byte) []byte {
	idBuf := WriteVarInt(id)
	body := append(idBuf, data...)
	return append(WriteVarInt(len(body)), body...)
}

// ReadPacketRaw reads a full length-prefixed Minecraft packet and returns the
// complete raw bytes (length prefix + body).  Callers can replay these bytes
// directly on a backend connection.
func ReadPacketRaw(r io.Reader) ([]byte, error) {
	length, err := ReadVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("mcproto: read packet length: %w", err)
	}
	if length < 0 || length > 2097151 { // 2 MiB sanity cap
		return nil, fmt.Errorf("mcproto: packet length %d out of range", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("mcproto: read packet body: %w", err)
	}
	full := append(WriteVarInt(length), body...)
	return full, nil
}

// StripPacketPrefix removes the length VarInt prefix from a raw packet and
// returns the body bytes.  This handles VarInts of any length (1-3 bytes).
func StripPacketPrefix(raw []byte) ([]byte, error) {
	r := bytes.NewReader(raw)
	_, err := ReadVarInt(r) // consume the length VarInt
	if err != nil {
		return nil, fmt.Errorf("mcproto: strip prefix: %w", err)
	}
	// The remaining bytes are the body.
	offset := len(raw) - r.Len()
	return raw[offset:], nil
}

// ParseLoginStart extracts the player name from a raw Login Start packet body
// (without the length prefix).  Body format: packet ID (0x00) + name (String).
func ParseLoginStart(body []byte) (string, error) {
	r := bytes.NewReader(body)

	pktID, err := ReadVarInt(r)
	if err != nil {
		return "", fmt.Errorf("mcproto: login start packet ID: %w", err)
	}
	if pktID != 0x00 {
		return "", fmt.Errorf("mcproto: expected login start 0x00, got 0x%02x", pktID)
	}

	name, err := ReadString(r)
	if err != nil {
		return "", fmt.Errorf("mcproto: login start name: %w", err)
	}
	return name, nil
}

// Handshake holds the parsed fields of a Minecraft Handshake packet (0x00).
type Handshake struct {
	ProtocolVersion int
	ServerAddress   string
	ServerPort      uint16
	NextState       int // 1 = status, 2 = login
}

// ParseHandshake parses a Handshake packet from raw body bytes (after length + packet ID).
func ParseHandshake(body []byte) (*Handshake, error) {
	r := bytes.NewReader(body)

	// Skip packet ID (already verified as 0x00 by caller).
	pktID, err := ReadVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("mcproto: handshake packet ID: %w", err)
	}
	if pktID != 0x00 {
		return nil, fmt.Errorf("mcproto: expected handshake packet ID 0x00, got 0x%02x", pktID)
	}

	h := &Handshake{}

	h.ProtocolVersion, err = ReadVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("mcproto: handshake protocol_version: %w", err)
	}

	h.ServerAddress, err = ReadString(r)
	if err != nil {
		return nil, fmt.Errorf("mcproto: handshake server_address: %w", err)
	}

	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(r, portBytes); err != nil {
		return nil, fmt.Errorf("mcproto: handshake server_port: %w", err)
	}
	h.ServerPort = binary.BigEndian.Uint16(portBytes)

	h.NextState, err = ReadVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("mcproto: handshake next_state: %w", err)
	}

	return h, nil
}

// ReadHandshake parses a Handshake packet from the reader.
// Assumes the packet length VarInt and packet ID (0x00) have already been consumed.
// Deprecated: prefer ReadPacketRaw + ParseHandshake for proxy replay support.
func ReadHandshake(r io.Reader) (*Handshake, error) {
	h := &Handshake{}
	var err error

	h.ProtocolVersion, err = ReadVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("mcproto: handshake protocol_version: %w", err)
	}

	h.ServerAddress, err = ReadString(r)
	if err != nil {
		return nil, fmt.Errorf("mcproto: handshake server_address: %w", err)
	}

	// Server port is an unsigned short (2 bytes, big-endian).
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(r, portBytes); err != nil {
		return nil, fmt.Errorf("mcproto: handshake server_port: %w", err)
	}
	h.ServerPort = binary.BigEndian.Uint16(portBytes)

	h.NextState, err = ReadVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("mcproto: handshake next_state: %w", err)
	}

	return h, nil
}

// StatusResponse builds a Server List Ping response packet.
// jsonPayload is the pre-formatted JSON status string.
func StatusResponse(jsonPayload string) []byte {
	return MakePacket(0x00, WriteString(jsonPayload))
}

// PongResponse builds a Pong packet (0x01) echoing back the 8-byte payload.
func PongResponse(payload []byte) []byte {
	return MakePacket(0x01, payload)
}

// LoginDisconnect builds a Login Disconnect packet with a JSON chat message.
func LoginDisconnect(jsonChat string) []byte {
	return MakePacket(0x00, WriteString(jsonChat))
}
