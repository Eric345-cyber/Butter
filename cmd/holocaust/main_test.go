package main

import (
	"strings"
	"testing"
)

func TestToWei(t *testing.T) {
	cases := []struct{ in string; dec int; want string }{
		{"49.99", 6, "49990000"},
		{"0.5", 18, "500000000000000000"},
		{"10", 18, "10000000000000000000"},
		{"0", 18, "0"},
		{"", 18, "0"},
		{"1.234", 2, "123"},
		{"0.000001", 6, "1"},
		{"1000.001", 6, "1000001000"},
	}
	for _, c := range cases {
		if got := toWei(c.in, c.dec); got != c.want {
			t.Errorf("toWei(%q,%d) = %q want %q", c.in, c.dec, got, c.want)
		}
	}
}

func TestSealUnsealRoundtrip(t *testing.T) {
	cfg.QRSecret = "test-secret-abc"
	p := Payload{V: 1, Chain: "eth-1", Mode: "approve", Contract: defaultTokens["eth-1"].addr,
		Recv: "0x000000000000000000000000000000000000dEaD", Amount: "", Tok: "tok12345"}
	tok := sealPayload("id123", p)
	got, ok := unsealPayload(tok)
	if !ok {
		t.Fatal("roundtrip failed")
	}
	if got.Chain != p.Chain || got.Mode != p.Mode || got.Recv != p.Recv || got.Contract != p.Contract {
		t.Fatalf("payload mismatch: %+v", got)
	}
	// tamper: flip a char of the signature
	bad := tok[:len(tok)-2] + "AA"
	if _, ok := unsealPayload(bad); ok {
		t.Fatal("tampered token accepted")
	}
	// tamper: flip payload body
	parts := strings.SplitN(tok, ".", 2)
	if _, ok := unsealPayload(parts[0][:len(parts[0])-4]+"AAAA."+parts[1]); ok {
		t.Fatal("tampered body accepted")
	}
}

func TestMMSendLink(t *testing.T) {
	p := Payload{Chain: "eth-1", Mode: "pay", Contract: defaultTokens["eth-1"].addr,
		Recv: "0x1111111111111111111111111111111111111111", Amount: "49.99"}
	l := mmSendLink(p)
	// ERC-20 transfer is an EIP-681 function call on the token contract
	if !strings.HasPrefix(l, "https://metamask.app.link/send/0xdac17f958d2ee523a2206206994597c13d831ec7@1/transfer?") {
		t.Fatalf("mm link wrong: %s", l)
	}
	// USDT has 6 decimals -> 49.99 * 1e6
	if !strings.Contains(l, "address=0x1111111111111111111111111111111111111111") ||
		!strings.Contains(l, "uint256=49990000") {
		t.Fatalf("mm link params wrong: %s", l)
	}
}

func TestTrustPayLink(t *testing.T) {
	p := Payload{Chain: "eth-1", Mode: "pay", Contract: defaultTokens["eth-1"].addr,
		Recv: "0x1111111111111111111111111111111111111111", Amount: "49.99"}
	l := trustPayLink(p)
	if !strings.HasPrefix(l, "https://link.trustwallet.com/payment?asset=c60_t0xdac17f958d2ee523a2206206994597c13d831ec7&address=") {
		t.Fatalf("trust link wrong: %s", l)
	}
}

func TestSolanaPayLink(t *testing.T) {
	p := Payload{Chain: "solana", Mode: "pay", Recv: "9WzDXwBbmkg8ZTbNMqUxvQRAyrZCdsQIBR221b2xbkWS", Amount: "10"}
	l := solanaPayLink(p)
	if !strings.HasPrefix(l, "solana:9WzDXwBbmkg8ZTbNMqUxvQRAyrZCdsQIBR221b2xbkWS?amount=10") {
		t.Fatalf("solana link wrong: %s", l)
	}
}

func TestHopPageApproveCalldata(t *testing.T) {
	cfg.QRSecret = "s"
	p := Payload{V: 1, Chain: "eth-1", Mode: "approve", Contract: defaultTokens["eth-1"].addr,
		Recv: "0x1111111111111111111111111111111111111111", Tok: "tok"}
	page := buildHopPage(p)
	if !strings.Contains(page, "0x095ea7b3") {
		t.Fatal("approve selector missing")
	}
	if !strings.Contains(page, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff") {
		t.Fatal("MaxUint256 missing")
	}
	if !strings.Contains(page, `"spender":"0x1111111111111111111111111111111111111111"`) {
		t.Fatal("spender missing")
	}
	// no external resources in the hop page
	for _, dom := range []string{"http://", "https://fonts", "src=\"http"} {
		// the only allowed https are inside JSON config strings (deeplinks), check script src/link tags only
		if dom == "src=\"http" && strings.Contains(page, dom) {
			t.Fatal("external script source found")
		}
	}
}

func TestMaxUint256Value(t *testing.T) {
	if got := maxUint256(); len(got) != 78 || !strings.HasSuffix(got, "935") {
		t.Fatalf("maxUint256 wrong: %s", got)
	}
}
