# Multi-Server Configuration (v2)

> **Status**: Specification — not yet implemented. Planned for `v2.0.0`.

---

## Use case

You have **multiple Minecraft servers** inside the same LXC, all managed by the same Crafty Controller instance. Each server has a public hostname (e.g. `survival.minecraft.example.com`, `creative.minecraft.example.com`).

When a player connects to a specific hostname, `mc-wake-proxy` routes them to the correct backend (port + Crafty `server_id`) — starting the server on demand if needed.

---

## Format (YAML)

```yaml
# servers.yml — mounted at /app/servers.yml inside the container
servers:
  - hostname: survival.minecraft.example.com
    backend: 192.168.1.131:25566
    crafty_server_id: abc12345-6789-4def-9012-3456789abcde

  - hostname: creative.minecraft.example.com
    backend: 192.168.1.131:25567
    crafty_server_id: def67890-abcd-4ef0-1234-567890abcdef
```

### Fields

| Field | Description |
|---|---|
| `hostname` | The exact hostname players type in Minecraft (case-insensitive match). |
| `backend` | `IP:port` of the Minecraft server process inside the LXC. |
| `crafty_server_id` | The Crafty Controller server UUID for this backend. |

---

## How it works

1. Player connects to `creative.minecraft.example.com`.
2. `mc-wake-proxy` parses the Handshake packet and extracts `serverAddress`.
3. It looks up `creative.minecraft.example.com` in the config file.
4. If the backend `192.168.1.131:25567` is offline, the standard wake chain runs:
   - WOL → Proxmox host (if down)
   - Start LXC (if stopped)
   - Start specific Minecraft server via Crafty (`crafty_server_id`)
5. Once ready, the proxy transparently forwards bytes.

---

## Shared infrastructure

Since all servers share:
- The **same Proxmox host** (one WOL MAC).
- The **same LXC** (one Proxmox LXC ID to check/start).
- The **same Crafty Controller** instance.

…the environment variables for Proxmox and Crafty remain **global**. Only the `hostname → (backend, server_id)` mapping changes per server.

---

## Fallback (v2 behaviour)

If no `servers.yml` is provided (or the file is absent), `mc-wake-proxy` falls back to the single-server configuration via environment variables (`BACKEND_TARGET`, `CRAFTY_SERVER_ID`). This preserves backward compatibility with v1.

---

## Notes

- This file is **optional**. When absent, the proxy operates in single-server mode (v1).
- Hostname matching is case-insensitive and exact (no wildcards; you can add multiple entries for variant spellings).
- Port binding is **single**: the proxy listens on **one TCP port** and routes by hostname, not by port. This is exactly how Minecraft Java Edition's server list works — the client always sends the hostname in the handshake, and the server can route accordingly.
