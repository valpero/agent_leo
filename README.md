# Valpero Agent

A lightweight server monitoring agent for [Valpero](https://valpero.com) — pushes CPU, RAM, disk, network, Docker, and process metrics to your dashboard every 30 seconds.

![Linux](https://img.shields.io/badge/Linux-amd64%20%7C%20arm64%20%7C%20armv7-FCC624?logo=linux&logoColor=black)
![systemd](https://img.shields.io/badge/init-systemd-black?logo=linux)
![Version](https://img.shields.io/badge/version-1.0.0-5b4cf5)

---

## Install

```bash
curl -sSL https://valpero.com/agent/install.sh | sudo bash -s -- --token=val_agnt_...
```

Get your token from [Dashboard → Servers → Add Server](https://valpero.com/dashboard/servers).

Your server will appear in the dashboard within 30 seconds.

---

## What it collects

| Metric | Detail |
|--------|--------|
| **CPU** | Overall % + per-core breakdown |
| **Load average** | 1m / 5m / 15m |
| **RAM** | Used / total / available |
| **Swap** | Used / total |
| **Disk** | Usage %, I/O read/write per mount |
| **Network** | RX / TX bytes per interface |
| **Processes** | Top processes by CPU & RAM |
| **Docker** | Container status, CPU, RAM per container |
| **Temperature** | CPU temperature (if available) |
| **Uptime** | System uptime in seconds |

---

## Requirements

- Linux (x86\_64, arm64, or armv7)
- systemd
- root / sudo for install

---

## Commands

```bash
# Install
curl -sSL https://valpero.com/agent/install.sh | sudo bash -s -- --token=val_agnt_...

# Update (preserves existing token)
curl -sSL https://valpero.com/agent/install.sh | sudo bash -s -- --update

# Uninstall
curl -sSL https://valpero.com/agent/install.sh | sudo bash -s -- --uninstall

# Status & logs
systemctl status valpero-agent
journalctl -u valpero-agent -f
```

---

## How it works

1. The installer downloads a pre-compiled binary matching your architecture (`linux/amd64`, `linux/arm64`, or `linux/armv7`)
2. Creates a `valpero` system user (no login shell, no home directory)
3. Stores the token in `/etc/valpero/agent.conf` (mode `600`, root-owned)
4. Registers and starts a systemd service with strict sandboxing (`NoNewPrivileges`, `ProtectSystem`, `PrivateTmp`)
5. The agent connects to `valpero.com` and pushes metrics every 30 seconds over HTTPS

---

## Files installed on your server

| Path | Description |
|------|-------------|
| `/usr/local/bin/valpero-agent` | Agent binary |
| `/etc/valpero/agent.conf` | Token config (mode 600) |
| `/etc/systemd/system/valpero-agent.service` | systemd unit |

---

## Security

- Runs as an unprivileged system user (`valpero`)
- Token stored with `chmod 600`, readable only by root and the service user
- systemd sandboxing: `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=read-only`, `PrivateTmp=yes`
- All communication over HTTPS to `valpero.com`
- Optionally added to the `docker` group for container monitoring — only if Docker is present on the machine

---

## License

MIT © 2026 Valpero
