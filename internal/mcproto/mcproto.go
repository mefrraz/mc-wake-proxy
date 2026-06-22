// Package mcproto implements minimal Minecraft protocol support for handshake,
// status response, and login disconnect.
package mcproto

import (
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

// Handshake holds the parsed fields of a Minecraft Handshake packet (0x00).
type Handshake struct {
	ProtocolVersion int
	ServerAddress   string
	ServerPort      uint16
	NextState       int // 1 = status, 2 = login
}

// ReadHandshake parses a Handshake packet from the reader.
// Assumes the packet length VarInt and packet ID (0x00) have already been consumed.
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
