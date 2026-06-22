# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.1.0] — 2025-06-22

### Fixed

- **Critical**: transparent proxy now replays captured Handshake + Login Start packets to the backend, fixing a bug where the Minecraft server never received the client's initial protocol handshake and connections silently timed out.

### Added

- `CRAFTY_SCHEME` environment variable (default `https`, set to `http` for plain-text Crafty setups).
- Player name extraction from Login Start packet — wake logs now show which player triggered the sequence (e.g. `WAKE: Steve triggered wake sequence`).
- `mcproto.ReadPacketRaw()` function for capturing raw Minecraft packets with length prefix.
- `mcproto.ParseHandshake()` for parsing handshake fields from raw bytes.
- `mcproto.ReadStringFromBytes()` helper.

### Changed

- `crafty.NewClient` now accepts a `scheme` parameter (`http` or `https`).
- Wake `StartingMC` phase now polls the Crafty API instead of only using TCP dial.
- `proxyToBackend` replays captured `hsRaw` and `lsRaw` bytes before `io.Copy`.

---

## [1.0.0] — 2025-06-22

### Added

- Wake-on-demand TCP proxy for Minecraft servers.
- Wake-flow state machine with 5 phases.
- Wake-on-LAN magic packet sender.
- Proxmox VE API client (LXC status + start).
- Crafty Controller API client (server status + start).
- Minecraft protocol primitives (VarInt, handshake, status, ping, disconnect).
- Transparent TCP byte forwarding.
- Web dashboard with phase-aware status and live logs.
- Multi-arch Dockerfile (ARM + x86).
- docker-compose.yml with documented environment variables.
- Documentation suite (README, setup guides, roadmap, multi-server spec).
- MIT License.
- 27 unit tests across 5 packages.
