# Proxmox VE API Token Setup

This guide walks you through creating a Proxmox VE API token with the **minimum permissions** required by `mc-wake-proxy`.

## Required permissions

`mc-wake-proxy` only needs two operations against one LXC:

| Operation | Proxmox API path | Required privilege |
|---|---|---|
| Read LXC status | `GET /nodes/{node}/lxc/{id}/status/current` | `VM.Audit` |
| Start LXC | `POST /nodes/{node}/lxc/{id}/status/start` | `VM.PowerMgmt` |

No other permissions are needed. This token cannot create, delete, or modify VMs/LXCs, access storage, or change system settings.

---

## Step-by-step

### 1. Log into Proxmox web interface

`https://<your-proxmox-host>:8006`

### 2. Open the API Tokens page

- Go to **Datacenter** → **Permissions** → **API Tokens**.
- Click **Add**.

### 3. Create the token

| Field | Value |
|---|---|
| **User** | `root@pam` (or another user with sufficient privileges) |
| **Token ID** | `mc-wake-proxy` (or any name you prefer) |
| **Privilege Separation** | ✓ **Enabled** (this is the default and ensures the token's permissions are a subset of the user's, not a superset) |
| **Expire** | Leave empty for no expiration (or set a date if you rotate tokens regularly) |

Click **Add**.

### 4. Copy the secret

After creation, Proxmox shows the token secret **once**. It looks like a UUID:

```
Token Secret: a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

**Copy this immediately** and store it somewhere safe. If you lose it, you must revoke and recreate the token.

### 5. Add permissions for the token

- Go to **Datacenter** → **Permissions** → **Add** → **API Token Permission**.
- Select the token you just created: `root@pam!mc-wake-proxy`.
- Configure:

| Field | Value |
|---|---|
| **Path** | `/nodes/{NODE_NAME}/lxc/{LXC_ID}` — e.g. `/nodes/pve/lxc/100` |
| **Role** | Select an existing role, or create a custom one (see below). |

### 6. Choosing the role

#### Option A: Use an existing role (simpler)

The built-in **PVEVMUser** role includes `VM.Audit` and `VM.PowerMgmt`, plus a few extra permissions you probably don't need (like `VM.Console`). If you're comfortable with this, assign **PVEVMUser**.

#### Option B: Create a custom role (principle of least privilege)

1. Go to **Datacenter** → **Permissions** → **Roles**.
2. Click **Create**.
3. Name it `MinecraftProxy`.
4. Under **Privileges**, select only:
   - **VM.Audit**
   - **VM.PowerMgmt**
5. Click **Create**.

Then assign this `MinecraftProxy` role to the token on path `/nodes/{NODE_NAME}/lxc/{LXC_ID}`.

### 7. Verify the token works

From any machine with `curl`:

```bash
curl -k -H "Authorization: PVEAPIToken root@pam!mc-wake-proxy=a1b2c3d4-..." \
  "https://<PROXMOX_HOST>:8006/api2/json/nodes/<NODE>/lxc/<ID>/status/current"
```

You should get a JSON response with `"data": {"status": "running"}` (or `"stopped"`).

---

## Environment variables

Based on the values above, set these in your `docker-compose.yml`:

```yaml
- PROXMOX_HOST=<your-proxmox-ip>
- PROXMOX_PORT=8006
- PROXMOX_NODE=<node-name>           # Run 'hostname' on Proxmox host to find this
- PROXMOX_LXC_ID=<lxc-id>            # e.g. 100
- PROXMOX_TOKEN_ID=root@pam!mc-wake-proxy
- PROXMOX_TOKEN_SECRET=a1b2c3d4-e5f6-7890-abcd-ef1234567890
- PROXMOX_INSECURE_SKIP_VERIFY=true  # Proxmox default cert is self-signed
```

> **Finding PROXMOX_NODE**: SSH into your Proxmox host and run `hostname`. Use exactly that value. It is **not** always `pve` — that's just a common default. Examples: `proserver`, `pve-node`, `proxmox`.

> **Security note**: `PROXMOX_INSECURE_SKIP_VERIFY=true` skips TLS certificate verification. This is acceptable on a **trusted home LAN** where the risk of MITM is negligible. If you have configured a legitimate TLS certificate on Proxmox, set this to `false`.
