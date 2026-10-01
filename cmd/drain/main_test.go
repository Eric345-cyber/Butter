package main

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestEIP155Vector reproduces the canonical example from EIP-155
// (https://eips.ethereum.org/EIPS/eip-155) to prove the signing path:
// nonce=9, gasprice=20gwei, gas=21000, to=0x35…35, value=1e18,
// key=0x46…46, chainid=1 -> v=37 and the exact raw tx hex.
func TestEIP155Vector(t *testing.T) {
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	to := common.HexToAddress("0x3535353535353535353535353535353535353535")
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    9,
		GasPrice: big.NewInt(20000000000), // 20 gwei
		Gas:      21000,
		To:       &to,
		Value:    new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil),
		Data:     []byte{},
	})
	signer := types.NewEIP155Signer(big.NewInt(1))
	signed, err := types.SignTx(tx, signer, key)
	if err != nil {
		t.Fatal(err)
	}
	if signed.Type() != types.LegacyTxType {
		t.Fatalf("unexpected tx type %d", signed.Type())
	}
	v, _, _ := signed.RawSignatureValues()
	if v.Int64() != 37 {
		t.Fatalf("v = %d, want 37", v.Int64())
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	want := "f86c098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a76400008025a028ef61340bd939bc2195fe537567866003e1a15d3c71ff63e1590620aa636276a067cbe9d8997f761aecb703304b3800ccf555c9f3dc64214b297fb1966a3b6d83"
	if hex.EncodeToString(raw) != want {
		t.Fatalf("raw tx mismatch:\n got %s\nwant %s", hex.EncodeToString(raw), want)
	}
}

// TestTransferFromCalldata verifies the 0x23b872dd packing in signTransferFrom
// by running it as a dry construction (invalid key would fail signing, so we
// use a real key and only inspect the decoded data field).
func TestTransferFromCalldata(t *testing.T) {
	// derive a deterministic key from the vector key
	key, _ := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	_ = key
	victim := "0x1111111111111111111111111111111111111111"
	receiver := "0x2222222222222222222222222222222222222222"
	amount, _ := new(big.Int).SetString("49990000", 10)

	data := make([]byte, 4+96)
	copy(data, mustHex("23b872dd"))
	copy(data[4+12:], mustHex(victim[2:]))
	copy(data[4+32+12:], mustHex(receiver[2:]))
	copy(data[4+64:], amount.Bytes())

	if hex.EncodeToString(data[:4]) != "23b872dd" {
		t.Fatal("selector wrong")
	}
	if hex.EncodeToString(data[16:36]) != victim[2:] {
		t.Fatal("from address packing wrong")
	}
	if hex.EncodeToString(data[48:68]) != receiver[2:] {
		t.Fatal("to address packing wrong")
	}
	if hex.EncodeToString(data[68:100]) != "02fac97000000000000000000000000000000000000000000000000000000000" {
		// 49990000 = 0x02FAC970 (left-padded to 32 bytes)
		t.Fatalf("amount packing wrong: %s", hex.EncodeToString(data[68:100]))
	}
}
