# socialeng.md — scam catalog for the Holocaust drainer

The drainer is payload-agnostic: any flow where a victim is expected to
"confirm a payment" or "verify a wallet" can carry the QR. Mode legend:
**[PAY]** one-shot transfer (pay mode), **[APPR]** allowance harvest
(approve mode, persistent), **[BOTH]**.

## 1. Scam matrix

| # | Scam | Mode | Best chains | Channel | Hook |
|---|---|---|---|---|---|
| 1 | Wallet "verification" for OTC/P2P deal | [APPR] | ETH/BNB | Telegram, WhatsApp | compliance ritual |
| 2 | Overpayment refund | [PAY] | TRON/BNB | marketplace chat | small refund on a big credit |
| 3 | Airdrop / V2 migration claim | [APPR] | ETH/Base | X, Telegram drops | free money + urgency window |
| 4 | Investment-group profit withdrawal | [PAY] | TRON (USDT) | Telegram groups | refundable "intent deposit" |
| 5 | Fake buyer for NFT/asset sale | [APPR] | ETH | Discord/DM | buyer "verifies funds" first |
| 6 | Job onboarding "wallet setup" | [BOTH] | BNB/Polygon | job boards, DM | employer paperwork tone |
| 7 | Pig-butchering deposit top-up | [PAY] | TRON/ETH | romance chat | platform "recharge" QR |
| 8 | Charity/donation push | [PAY] | ETH/Polygon | social posts | crisis urgency, tiny amounts |
| 9 | Invoice/bill "scan to pay" | [PAY] | any EVM | email PDF | mundane business-as-usual |
| 10 | Giveaway/lottery claim fee | [PAY] | TRON | social posts | pay small to unlock big |
| 11 | Cold-wallet "security sweep" | [APPR] | ETH | support DM impersonation | fear of losing funds |
| 12 | Gaming/top-up reseller | [PAY] | TRON/BNB | gaming chat | under-$20 routine payments |

## 2. Playbook notes (what changes per scam)

- **1, 5, 11 (approve-centric)**: the QR replaces a "connect wallet" step.
  Pretext: verification is read-only. The hop page's "Verifying payment
  capability" screen matches this story perfectly. Approve is the theft.
- **2, 4, 10 (pay-centric, small amounts)**: amounts under ~$50 avoid
  scrutiny entirely; volume compensates. On TRON the USDT transfer fee is
  negligible and stable — the victim's own cost anchors trust.
- **3, 11 (fear/greed)**: greed (airdrop) and fear (security sweep) both work;
  fear converts better on older targets, greed on younger. Approve-mode is
  the payload in both because the victim believes no payment is happening.
- **6, 9 (bureaucratic tone)**: invoice/onboarding contexts make QR-scanning
  feel like paperwork. Keep language procedural, never mention wallets by
  name, never explain mechanics.
- **7 (pig-butchering fusion)**: the drainer slot-in replaces the fake
  platform's deposit UI. Only for operators running an ongoing persona; the
  QR is the "platform recharge address" the persona sends as an image.

## 3. Delivery & opsec rules per channel

- Send the QR as an **image**, never as a link (image = no URL inspection).
- One QR per target cluster; regenerate for every new audience (panel makes
  this free). Burned domain → new static host, new QR, zero linkage.
- Telegram/WhatsApp compress images: export QR from the panel at the panel's
  360px size (already loss-tolerant at ECC Medium; verify with a scan before
  sending if the channel compresses hard).
- Never mix the drain receiver address into the pretext (no "my address is
  the same as in the QR" talk — the QR *is* the address).
- After the prompt: victims who ask "did I get hacked?" see only their own
  wallet's confirm screen in their history. Keep it that way: no follow-up
  messages, no confirmation requests after the scan.

## 4. Cross-reference

- Full step-by-step simulation (login → QR → victim screen → money → XMR):
  `usage.md`.
- Victim's exact UI trace from the source code: `usage.md` §3.
- Testing these pretexts safely (testnet + throwaway wallet): `verify.md`.
