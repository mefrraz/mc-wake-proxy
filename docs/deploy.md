# Deploying to Raspberry Pi

This guide covers all the ways to get `mc-wake-proxy` onto a Raspberry Pi and running.

---

## Option 1: Git clone directly on the Pi (easiest)

Since the repo is public on GitHub, just clone it on the Pi — no file transfer needed from your PC.

```bash
# SSH into the Pi, then:
cd /opt/docker-media
git clone https://github.com/mefrraz/mc-wake-proxy.git
cd mc-wake-proxy
```

Edit `docker-compose.yml` with your real values:

```bash
nano docker-compose.yml   # Replace every <REPLACE_ME>
```

Build and start:

```bash
docker compose build
docker compose up -d
docker compose logs -f
```

**To update later:**

```bash
cd /opt/docker-media/mc-wake-proxy
git pull
docker compose down
docker compose build
docker compose up -d
```

---

## Option 2: SCP from Windows PowerShell

If you make local changes and need to copy from Windows to the Pi, use the built-in `scp`:

```powershell
# In PowerShell (Windows 10+):
scp -r C:\Users\andre\OneDrive\Documentos\mc-wake-proxy\* pi@192.168.1.200:/opt/docker-media/mc-wake-proxy/
```

> **Note**: `rsync` does not exist on Windows. Use `scp` or `pscp` (from PuTTY) instead.

---

## Option 3: PSCP from PuTTY

If `scp` is not available in PowerShell, download `pscp.exe` from [PuTTY's site](https://www.chiark.greenend.org.uk/~sgtatham/putty/latest.html) and run:

```powershell
pscp -r C:\Users\andre\OneDrive\Documentos\mc-wake-proxy\* pi@192.168.1.200:/opt/docker-media/mc-wake-proxy/
```

---

## Option 4: Cross-compile on PC, push to registry

If your PC is x86 and you want to pre-build the image:

```bash
# On PC:
docker buildx create --use
docker buildx build --platform linux/arm64 -t mefrraz/mc-wake-proxy:v1.1.0 --push .

# On Pi:
docker pull mefrraz/mc-wake-proxy:v1.1.0
```

But building directly on the Pi with `docker compose build` works fine — the Dockerfile supports ARM natively.

---

## First run checklist

```bash
cd /opt/docker-media/mc-wake-proxy

# 1. Configure
nano docker-compose.yml    # Replace all <REPLACE_ME>

# 2. Build and launch
docker compose down
docker compose build
docker compose up -d

# 3. Watch logs
docker compose logs -f
```

### Verify everything works

| Test | How |
|---|---|
| Dashboard | Browser → `http://<pi-ip>:8080` |
| Minecraft status | Open Minecraft → Add Server → `<pi-ip>:25565` — should show MOTD |
| Wake chain | Click "Join Server" while host is sleeping — watch logs for WOL → Proxmox → Crafty → Ready |

---

## Troubleshooting

| Symptom | Check |
|---|---|
| Container exits immediately | `docker compose logs` — likely missing required env vars |
| WOL doesn't wake host | Pi and Proxmox must be on same subnet; verify `WOL_MAC` and `WOL_BROADCAST` |
| Proxmox API returns 401/403 | Verify `PROXMOX_TOKEN_ID` and `PROXMOX_TOKEN_SECRET`; test with curl from `docs/proxmox-setup.md` |
| Crafty API returns error | Verify `CRAFTY_TOKEN` and `CRAFTY_SERVER_ID`; ensure `show_status` is ON in Crafty for that server |
| Players can't join after wake | Verify `BACKEND_TARGET` is the correct IP:port of the Minecraft server inside the LXC |
| "Server is waking up" but never finishes | Check `COOLDOWN_MINUTES` — increase if the LXC + Minecraft takes >5 min to start |
