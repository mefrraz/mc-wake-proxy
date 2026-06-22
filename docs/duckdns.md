# DuckDNS Setup Guide

This guide walks you through setting up DuckDNS so players can reach your Minecraft servers from the internet using a friendly domain name.

---

## 1. Register a subdomain

1. Go to [duckdns.org](https://duckdns.org)
2. Sign in with GitHub, Google, or email
3. Create a subdomain (e.g. `mefrraz`)
4. Your full domain will be `mefrraz.duckdns.org`

---

## 2. Install the update client

DuckDNS needs to know your current public IP. Install the update script on your Raspberry Pi:

```bash
# Create the DuckDNS directory
mkdir -p ~/duckdns
cd ~/duckdns

# Create the update script
cat > duck.sh << 'EOF'
#!/bin/bash
echo url="https://www.duckdns.org/update?domains=YOUR_SUBDOMAIN&token=YOUR_TOKEN&ip=" | curl -k -o ~/duckdns/duck.log -K -
EOF

# Replace YOUR_SUBDOMAIN and YOUR_TOKEN with your actual values
# Token is shown on the DuckDNS page after you log in

chmod +x duck.sh

# Test it
./duck.sh
cat duck.log  # Should say "OK"
```

Add to crontab to run every 5 minutes:

```bash
crontab -e
# Add this line:
*/5 * * * * ~/duckdns/duck.sh >/dev/null 2>&1
```

---

## 3. Port forwarding

On your router:

| Setting | Value |
|---|---|
| **External port** | 25565 |
| **Internal IP** | Your Raspberry Pi's local IP (e.g. `192.168.1.200`) |
| **Internal port** | 25565 |
| **Protocol** | TCP |

---

## 4. Wildcard domains (for multi-server)

DuckDNS supports wildcards automatically. If your subdomain is `mefrraz`, all of these resolve to the same IP:

- `mefrraz.duckdns.org`
- `survival.mefrraz.duckdns.org`
- `creative.mefrraz.duckdns.org`
- `teste.mefrraz.duckdns.org`

No extra configuration needed — DuckDNS resolves `*.mefrraz.duckdns.org` to your public IP.

> The router only forwards port 25565 once. The proxy routes different hostnames to different backends via the Minecraft handshake protocol.

---

## 5. Configure mc-wake-proxy

On the dashboard **Settings** tab, use the **Discover from Crafty** button to import your servers. When prompted for a hostname, use your DuckDNS subdomain:

- Main server → `mefrraz.duckdns.org`
- Survival server → `survival.mefrraz.duckdns.org`
- Creative server → `creative.mefrraz.duckdns.org`

The DuckDNS section on the Settings page will suggest subdomains for each server.

---

## 6. Test

From **outside** your home network (e.g. mobile data):

1. Open Minecraft
2. Add Server → `mefrraz.duckdns.org` (or your subdomain)
3. It should show the MOTD and let you connect

---

## Troubleshooting

| Problem | Check |
|---|---|
| "Can't resolve hostname" | Wait 5 min for DuckDNS to propagate; verify `duck.sh` returns "OK" |
| "Can't connect to server" | Verify port forwarding on router; check that mc-wake-proxy is running |
| Only works on LAN, not from outside | Some routers don't support NAT loopback — test from mobile data |
| Wrong IP on DuckDNS | Your ISP may be using CGNAT. Check if your public IP matches what DuckDNS shows |
