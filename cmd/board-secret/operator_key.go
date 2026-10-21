package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/scrypt"
	"golang.org/x/term"
)

// The operator key is the trust root of the whole handoff scheme, and the
// operator's machine may well also run an agent under the same uid (it does
// in this deployment). A plaintext key file would make every "operator
// signature" only as strong as that agent's prompt hygiene. So the private
// half never touches disk unencrypted: scrypt(passphrase) -> secretbox.
// The passphrase is read from the terminal at keygen/sign time — an agent
// driving the CLI non-interactively can't supply it.

type encryptedOperatorKey struct {
	V          int    `json:"v"` // 1
	Salt       []byte `json:"salt"`
	Nonce      []byte `json:"nonce"`
	Sealed     []byte `json:"sealed"` // secretbox(privkey)
	PublicKey  []byte `json:"pub"`
	ScryptN    int    `json:"scrypt_n"`
	ScryptR    int    `json:"scrypt_r"`
	ScryptP    int    `json:"scrypt_p"`
	Deprecated bool   `json:"-"`
}

const (
	scryptN = 1 << 17 // ~100ms-class on a laptop; the file is long-lived
	scryptR = 8
	scryptP = 1
)

func deriveKey(passphrase, salt []byte, n, r, p int) (*[32]byte, error) {
	kb, err := scrypt.Key(passphrase, salt, n, r, p, 32)
	if err != nil {
		return nil, err
	}
	var key [32]byte
	copy(key[:], kb)
	return &key, nil
}

// readPassphrase prompts on the controlling terminal. It deliberately
// refuses to read from a pipe: the passphrase must come from a human at a
// keyboard, not from a script or an agent's stdin.
func readPassphrase(prompt string, confirm bool) ([]byte, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		tty, err := os.Open("/dev/tty")
		if err != nil {
			return nil, errors.New("operator passphrase requires an interactive terminal")
		}
		defer tty.Close()
		fmt.Fprint(os.Stderr, prompt)
		pass, err := term.ReadPassword(int(tty.Fd()))
		fmt.Fprintln(os.Stderr)
		return pass, err
	}
	fmt.Fprint(os.Stderr, prompt)
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, err
	}
	if confirm {
		fmt.Fprint(os.Stderr, "repeat: ")
		again, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return nil, err
		}
		if string(pass) != string(again) {
			return nil, errors.New("passphrases do not match")
		}
	}
	if len(pass) < 16 {
		return nil, errors.New("passphrase must be at least 16 characters — the key file is offline-attackable, so use a long phrase (e.g. diceware)")
	}
	return pass, nil
}

func sealOperatorKey(pub ed25519.PublicKey, priv ed25519.PrivateKey, passphrase []byte) (*encryptedOperatorKey, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key, err := deriveKey(passphrase, salt, scryptN, scryptR, scryptP)
	if err != nil {
		return nil, err
	}
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	sealed := secretbox.Seal(nil, priv, &nonce, key)
	return &encryptedOperatorKey{
		V: 1, Salt: salt, Nonce: nonce[:], Sealed: sealed, PublicKey: pub,
		ScryptN: scryptN, ScryptR: scryptR, ScryptP: scryptP,
	}, nil
}

func openOperatorKey(path string, passphrase []byte) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var enc encryptedOperatorKey
	if err := json.Unmarshal(data, &enc); err != nil || enc.V != 1 {
		// Legacy plaintext format ({"pub":..., "priv":...}): refuse with a
		// pointer to re-keygen rather than silently keep using it.
		var legacy map[string][]byte
		if err2 := json.Unmarshal(data, &legacy); err2 == nil && len(legacy["priv"]) == ed25519.PrivateKeySize {
			return nil, nil, errors.New("this operator key is stored UNENCRYPTED (old format) — generate a new one with operator keygen and re-sign the bundles")
		}
		return nil, nil, fmt.Errorf("unrecognized operator key file: %v", err)
	}
	key, err := deriveKey(passphrase, enc.Salt, enc.ScryptN, enc.ScryptR, enc.ScryptP)
	if err != nil {
		return nil, nil, err
	}
	var nonce [24]byte
	copy(nonce[:], enc.Nonce)
	priv, ok := secretbox.Open(nil, enc.Sealed, &nonce, key)
	if !ok {
		return nil, nil, errors.New("wrong passphrase (or corrupted key file)")
	}
	return enc.PublicKey, ed25519.PrivateKey(priv), nil
}
