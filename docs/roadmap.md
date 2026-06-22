# Roadmap

## v1.0.0 — First functional release ✅

**Goal**: Single-server wake-on-demand proxy, fully operational from WOL to transparent forwarding.

- [x] Project scaffold (Go module, package structure)
- [x] Wake-flow state machine (Idle → WakingHost → WaitingLXC → StartingMC → Ready)
- [x] Wake-on-LAN client (magic packet)
- [x] Proxmox VE API client (LXC status + start)
- [x] Crafty Controller API client (server status + start)
- [x] Minecraft protocol (handshake parsing, status response, login disconnect)
- [x] Proxy core (TCP listener, wake chain orchestration, transparent forwarding)
- [x] Web dashboard (phase status, elapsed time, live logs)
- [x] Dockerfile (multi-arch: ARM + x86)
- [x] docker-compose.yml with documented environment variables
- [x] Documentation (README, Proxmox setup, Crafty setup, multi-server spec)

## v1.1.0 — Player experience improvements

- [ ] Server icon (favicon) in offline MOTD responses
- [ ] More granular phase messages (which phase is currently active shown in MOTD)
- [ ] Configurable wait time per phase
- [ ] Better kick messages with estimated time remaining

## v1.2.0 — Dashboard polish

- [ ] Player list display during online state (poll Crafty API)
- [ ] World seed display
- [ ] Auto-shutdown when idle (no players for N minutes)
- [ ] Admin action buttons (stop server, restart server)
- [ ] Dark/light theme toggle

## v2.0.0 — Multi-server routing

- [ ] YAML config file for hostname → backend mapping
- [ ] Routing by `serverAddress` from Minecraft handshake
- [ ] Per-server independent wake state
- [ ] Dashboard shows all configured servers
- [ ] Full backward compatibility with v1 single-server mode

## v2.1.0+

- [ ] Prometheus metrics endpoint
- [ ] Health check endpoint for container orchestration
- [ ] Webhook notifications (Discord, etc.) on server state changes
