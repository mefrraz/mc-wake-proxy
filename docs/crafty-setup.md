# Crafty Controller API Setup

This guide explains how to create an API token in Crafty Controller and find your Minecraft server's `server_id`.

---

## 1. Enable `show_status` on your server

`mc-wake-proxy` uses the Crafty `/servers/status/` endpoint, which only returns servers with the **Show Status** flag enabled.

1. Log into your Crafty web interface (`https://<crafty-host>:8443`).
2. Go to **Dashboard** and select your Minecraft server.
3. Open **Server Configuration** → **Server Settings**.
4. Find the **Show Status** toggle and turn it **ON**.
5. Save.

---

## 2. Generate an API token

1. In Crafty, click your **username** (top-right corner) → **API Tokens**.
2. Click **Create API Token**.
3. Give it a descriptive name (e.g. `mc-wake-proxy`).
4. Select **Full Access** (the simplest approach — the token can manage all servers).
   > If you prefer limited access, create a role with permissions only for the specific server(s) and assign the token to that role. The minimum required permission is **Commands** on the target server(s).
5. Click **Create**.
6. **Copy the token immediately** — it will not be shown again.

The token is a long alphanumeric string.

---

## 3. Find your `server_id`

Each Minecraft server managed by Crafty has a unique UUID.

### Method A: From the URL

1. Go to your Crafty dashboard.
2. Click on the server you want to manage.
3. Look at the browser URL bar:

```
https://<crafty-host>:8443/panel/server_detail?id=abc12345-6789-...
```

The `id=` parameter is your `server_id`.

### Method B: From the API (after creating a token)

```bash
curl -k -H "Authorization: Bearer <YOUR_TOKEN>" \
  "https://<CRAFTY_HOST>:8443/api/v2/servers/status/"
```

Look for your server in the response and note the `"id"` field.

---

## 4. Verify

Test that your token works and your server is visible:

```bash
curl -k -H "Authorization: Bearer <YOUR_TOKEN>" \
  "https://<CRAFTY_HOST>:8443/api/v2/servers/status/" | jq .
```

You should see your server in the JSON array with `"id"`, `"running"`, `"online"`, etc.

---

## Environment variables

Based on the values obtained:

```yaml
- CRAFTY_HOST=<lxc-ip>              # e.g. 192.168.1.131
- CRAFTY_PORT=8443                   # Crafty default HTTPS port
- CRAFTY_TOKEN=<your-api-token>
- CRAFTY_SERVER_ID=<your-server-uuid>
- CRAFTY_INSECURE_SKIP_VERIFY=true   # Crafty default cert is self-signed
```

> **Note**: If your Crafty uses HTTP instead of HTTPS (port 8000), set `CRAFTY_PORT=8000` and the proxy will use `http://` automatically. The client always uses the HTTPS URL scheme; for plain HTTP, omit `CRAFTY_INSECURE_SKIP_VERIFY` or set it to the appropriate value.

---

## API endpoints used by mc-wake-proxy

| Purpose | Method | Path |
|---|---|---|
| Get runtime status | `GET` | `/api/v2/servers/status/` |
| Start a server | `POST` | `/api/v2/servers/{server_id}/action/start_server` |

Authentication: `Authorization: Bearer <token>` header.
