# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.0.0] — 2025-06-22

### Added

- Wake-on-demand TCP proxy for Minecraft servers.
- Wake-flow state machine with 5 phases: Idle, WakingHost, WaitingLXC, StartingMC, Ready.
- Wake-on-LAN magic packet sender.
- Proxmox VE API client (LXC status + start, API token auth).
- Crafty Controller API client (server status + start, Bearer token auth).
- Minecraft protocol primitives: VarInt, handshake parser (extracts `serverAddress` for future multi-server routing), Server List Ping response with dynamic MOTD, Login Disconnect with friendly kick messages.
- Transparent TCP byte forwarding between client and backend.
- Web dashboard with phase-aware status display and live log viewer.
- Multi-arch Dockerfile (ARM + x86) with multi-stage build.
- docker-compose.yml with all environment variables documented.
- Documentation: README (with Mermaid architecture diagram), Proxmox setup guide, Crafty setup guide, multi-server specification (v2), roadmap.
- MIT License.
- Unit tests for state machine, WOL, Proxmox client, Crafty client, Minecraft protocol (25 tests total).
