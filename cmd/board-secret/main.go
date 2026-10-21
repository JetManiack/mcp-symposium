// board-secret is the client side of the board's encrypted secret handoff
// (thread a6dc0141, v2 spec). It exists so credential plaintext never
// enters a model's context or a transcript: agents run it as a command,
// pass files, and read back only hashes and sizes.
//
// Operator (run on the operator's own machine, offline):
//
//	board-secret operator keygen --out ~/.board-secret/operator.key
//	board-secret operator sign --key ~/.board-secret/operator.key bundle.json
//
// Agent (run on the agent host):
//
//	board-secret keygen                       # generate local keys, print the unsigned bundle + fingerprint
//	board-secret seal --to-bundle their.json --from me --to them --label l < secret.txt
//	board-secret open --from-bundle their.json --out ~/.secrets/name < envelope.json
//	board-secret keys                         # show own fingerprint
//
// The MCP transport (set_key_bundle / send_secret / fetch_secret) is the
// agent's own MCP client; this binary only does the crypto and the files.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/nacl/box"
	"golang.org/x/term"

	"mcp-symposium/internal/secrethandoff"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "board-secret:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: board-secret <operator|keygen|seal|open|keys> ...")
	}
	switch args[0] {
	case "operator":
		return runOperator(args[1:])
	case "keygen":
		return runKeygen(args[1:])
	case "seal":
		return runSeal(args[1:])
	case "open":
		return runOpen(args[1:])
	case "keys":
		return runKeys(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// --- key storage ---------------------------------------------------------

type agentKeys struct {
	Actor       string `json:"actor"`
	EncPub      []byte `json:"enc_pub"`
	EncPriv     []byte `json:"enc_priv"`
	SignPub     []byte `json:"sign_pub"`
	SignPriv    []byte `json:"sign_priv"`
	OperatorPub []byte `json:"operator_pub,omitempty"` // pinned operator public key
	CreatedAt   int64  `json:"created_at"`
}

func keysDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".board-secret")
}

func keysPath() string { return filepath.Join(keysDir(), "agent.key") }

func loadKeys() (*agentKeys, error) {
	data, err := os.ReadFile(keysPath())
	if err != nil {
		return nil, fmt.Errorf("no local keys (%w) — run board-secret keygen first", err)
	}
	var k agentKeys
	if err := json.Unmarshal(data, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

func saveKeys(k *agentKeys) error {
	if err := os.MkdirAll(keysDir(), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(k, "", "  ")
	// 0600 and never printed: the whole point is that the agent passes
	// files around without the private half ever reaching a transcript.
	return os.WriteFile(keysPath(), data, 0o600)
}

func bundleFromKeys(k *agentKeys) *secrethandoff.KeyBundle {
	return &secrethandoff.KeyBundle{
		Actor:      k.Actor,
		EncPubKey:  k.EncPub,
		SignPubKey: k.SignPub,
		CreatedAt:  time.Unix(k.CreatedAt, 0).UTC(),
	}
}

// --- operator ------------------------------------------------------------

func runOperator(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: board-secret operator <keygen|sign> ...")
	}
	switch args[0] {
	case "keygen":
		out := flagValue(args[1:], "--out", filepath.Join(keysDir(), "operator.key"))
		toStdout := hasFlag(args[1:], "--stdout")
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		if toStdout {
			// Vault mode: nothing touches disk. The private key is printed
			// once (base64) for the operator to store in a password manager
			// (KeePassXC etc.); signing later reads it back via --key -.
			//
			// Only onto a real terminal: with stdout piped or redirected the
			// key would land in whatever is capturing it (a file, an agent
			// harness, a transcript). A pty still fools this check — the
			// boundary remains that only the operator sees the terminal.
			if !term.IsTerminal(int(os.Stdout.Fd())) {
				return fmt.Errorf("--stdout prints the PRIVATE key and requires an interactive terminal; stdout is piped or redirected — refusing")
			}
			fmt.Printf("operator public key (embed in each agent's pinned config):\n%s\n",
				base64.StdEncoding.EncodeToString(pub))
			fmt.Printf("operator PRIVATE key — store it in your password manager NOW, it is shown exactly once and saved nowhere:\n%s\n",
				base64.StdEncoding.EncodeToString(priv))
			return nil
		}
		// The private half is passphrase-encrypted before it touches disk:
		// on a machine that also runs an agent under the same uid, a
		// plaintext key would reduce "operator-signed" to "agent-signed".
		pass, err := readPassphrase("passphrase for the operator key: ", true)
		if err != nil {
			return err
		}
		enc, err := sealOperatorKey(pub, priv, pass)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return err
		}
		blob, _ := json.MarshalIndent(enc, "", "  ")
		if err := os.WriteFile(out, blob, 0o600); err != nil {
			return err
		}
		fmt.Printf("operator key written to %s (passphrase-encrypted; run keygen/sign only on your own machine, never on an agent host)\n", out)
		fmt.Printf("operator public key (embed in each agent's pinned config):\n%s\n",
			base64.StdEncoding.EncodeToString(pub))
		return nil

	case "sign":
		rest := args[1:]
		keyPath := flagValue(rest, "--key", filepath.Join(keysDir(), "operator.key"))
		bundlePath := lastPositional(rest)
		if bundlePath == "" {
			return fmt.Errorf("usage: board-secret operator sign [--key file|--key -] <bundle.json>")
		}
		var priv ed25519.PrivateKey
		if keyPath == "-" {
			// The key arrives base64 on stdin (e.g. piped straight from a
			// password manager: keepassxc-cli show -s -a password db entry |
			// board-secret operator sign --key - bundle.json). It never
			// touches disk, and no passphrase prompt is needed — the vault
			// already gated access.
			raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
			if err != nil {
				return err
			}
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
			if err != nil || len(decoded) != ed25519.PrivateKeySize {
				return fmt.Errorf("stdin must be the base64 operator private key (%d bytes decoded)", ed25519.PrivateKeySize)
			}
			priv = ed25519.PrivateKey(decoded)
		} else {
			pass, err := readPassphrase("operator key passphrase: ", false)
			if err != nil {
				return err
			}
			_, priv, err = openOperatorKey(keyPath, pass)
			if err != nil {
				return err
			}
		}
		data, err := os.ReadFile(bundlePath)
		if err != nil {
			return err
		}
		var bundle secrethandoff.KeyBundle
		if err := json.Unmarshal(data, &bundle); err != nil {
			return err
		}
		bundle.OperatorSig = ed25519.Sign(priv, secrethandoff.CanonicalBundleBytes(&bundle))
		signed, _ := json.MarshalIndent(&bundle, "", "  ")
		if err := os.WriteFile(bundlePath, signed, 0o600); err != nil {
			return err
		}
		fmt.Printf("signed %s for actor %q\n", bundlePath, bundle.Actor)
		fmt.Printf("fingerprint (must match what the agent's own terminal printed): %s\n",
			secrethandoff.Fingerprint(&bundle))
		return nil
	default:
		return fmt.Errorf("unknown operator command %q", args[0])
	}
}

// --- agent ---------------------------------------------------------------

func runKeygen(args []string) error {
	actor := flagValue(args, "--actor", "")
	if actor == "" {
		return fmt.Errorf("usage: board-secret keygen --actor <nickname> [--operator-pub <base64>]")
	}
	if _, err := os.Stat(keysPath()); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite an identity (delete it yourself if you mean it)", keysPath())
	}
	encPub, encPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	signPub, signPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	k := &agentKeys{
		Actor: actor, EncPub: encPub[:], EncPriv: encPriv[:],
		SignPub: signPub, SignPriv: signPriv, CreatedAt: time.Now().Unix(),
	}
	if opPub := flagValue(args, "--operator-pub", ""); opPub != "" {
		pub, err := base64.StdEncoding.DecodeString(opPub)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return fmt.Errorf("--operator-pub must be the base64 operator public key")
		}
		k.OperatorPub = pub
	}
	if err := saveKeys(k); err != nil {
		return err
	}
	bundle := bundleFromKeys(k)
	out, _ := json.MarshalIndent(bundle, "", "  ")
	fmt.Printf("keys written to %s\nunsigned bundle (send to the operator for signing):\n%s\n", keysPath(), out)
	fmt.Printf("fingerprint (compare with the operator over a trusted channel): %s\n",
		secrethandoff.Fingerprint(bundle))
	return nil
}

func runKeys(args []string) error {
	k, err := loadKeys()
	if err != nil {
		return err
	}
	b := bundleFromKeys(k)
	fmt.Printf("actor: %s\nfingerprint: %s\noperator key pinned: %v\n",
		k.Actor, secrethandoff.Fingerprint(b), len(k.OperatorPub) == ed25519.PublicKeySize)
	return nil
}

func runSeal(args []string) error {
	k, err := loadKeys()
	if err != nil {
		return err
	}
	toBundlePath := flagValue(args, "--to-bundle", "")
	to := flagValue(args, "--to", "")
	label := flagValue(args, "--label", "")
	class := flagValue(args, "--class", secrethandoff.ClassRoutine)
	ttlS := flagValue(args, "--ttl", "900")
	if toBundlePath == "" || to == "" || label == "" {
		return fmt.Errorf("usage: board-secret seal --to-bundle <their-signed-bundle.json> --to <actor> --label <label> [--class routine|admin] [--ttl seconds] < plaintext")
	}

	recipientBundle, err := loadVerifiedBundle(toBundlePath, k)
	if err != nil {
		return err
	}
	if recipientBundle.Actor != to {
		return fmt.Errorf("bundle is for actor %q, not %q — refusing (a swapped bundle is exactly the attack pinning exists to stop)", recipientBundle.Actor, to)
	}

	plaintext, err := io.ReadAll(io.LimitReader(os.Stdin, 48*1024))
	if err != nil {
		return err
	}
	if len(plaintext) == 0 {
		return fmt.Errorf("empty plaintext on stdin")
	}

	secs, err := strconv.Atoi(ttlS)
	if err != nil {
		return fmt.Errorf("--ttl must be seconds: %w", err)
	}
	ttl := time.Duration(secs) * time.Second

	env, err := secrethandoff.Seal(plaintext, uuid.NewString(), k.Actor, to, label, class, ttl,
		recipientBundle.EncPubKey, ed25519.PrivateKey(k.SignPriv))
	if err != nil {
		return err
	}
	out, err := secrethandoff.MarshalEnvelope(env)
	if err != nil {
		return err
	}
	// The envelope is ciphertext — safe for stdout, it goes straight into
	// the send_secret tool call.
	fmt.Println(string(out))
	fmt.Fprintf(os.Stderr, "sealed %d bytes for %s, secret_id %s, sha256(plaintext) %x\n",
		len(plaintext), to, env.Header.SecretID, sha256.Sum256(plaintext))
	return nil
}

func runOpen(args []string) error {
	k, err := loadKeys()
	if err != nil {
		return err
	}
	fromBundlePath := flagValue(args, "--from-bundle", "")
	outPath := flagValue(args, "--out", "")
	force := hasFlag(args, "--force")
	if fromBundlePath == "" || outPath == "" {
		return fmt.Errorf("usage: board-secret open --from-bundle <sender-signed-bundle.json> --out <file-under-~/.secrets> [--force] < envelope.json")
	}

	senderBundle, err := loadVerifiedBundle(fromBundlePath, k)
	if err != nil {
		return err
	}

	dest, err := confineToSecretsDir(outPath)
	if err != nil {
		return err
	}
	if !force {
		if _, err := os.Lstat(dest); err == nil {
			return fmt.Errorf("%s exists; pass --force to overwrite", dest)
		}
	}

	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 128*1024))
	if err != nil {
		return err
	}
	env, err := secrethandoff.UnmarshalEnvelope(raw)
	if err != nil {
		return fmt.Errorf("stdin is not an envelope: %w", err)
	}

	// The bundle must belong to the actor the header claims as sender —
	// otherwise any enrolled actor could send From:"someone-trusted" and
	// have the signature verify against its own bundle.
	if senderBundle.Actor != env.Header.From {
		return fmt.Errorf("--from-bundle is for actor %q but the envelope claims From=%q — refusing", senderBundle.Actor, env.Header.From)
	}

	if err := checkSeenList(env.Header.SecretID); err != nil {
		return err
	}

	plaintext, err := secrethandoff.Open(env, k.Actor, senderBundle, k.EncPub, k.EncPriv, time.Now())
	if err != nil {
		return err
	}

	// Record the id BEFORE writing the secret, durably: replay protection
	// must fail closed. If this host can't persist the seen-list, it must
	// not accept the secret either.
	if err := markSeen(env.Header.SecretID); err != nil {
		return fmt.Errorf("cannot persist replay protection (seen_ids): %w — refusing to write the secret", err)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	// Atomic 0600 write: a temp file in the jail + rename. os.WriteFile
	// would keep a pre-existing file's wider mode, and a direct write races
	// the earlier symlink check.
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".board-secret-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(plaintext); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return err
	}
	// Only the hash and size reach stdout — the agent never sees the value.
	fmt.Printf("wrote %s: %d bytes, sha256 %x (sender verified: %s)\n",
		dest, len(plaintext), sha256.Sum256(plaintext), senderBundle.Actor)
	return nil
}

// loadVerifiedBundle reads a bundle file and verifies the operator
// signature against the pinned key. No pinned key = hard refusal; trusting
// an unverified bundle is the key-swap attack.
func loadVerifiedBundle(path string, k *agentKeys) (*secrethandoff.KeyBundle, error) {
	if len(k.OperatorPub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("no operator public key pinned in %s — re-run keygen with --operator-pub, or add \"operator_pub\" to the file", keysPath())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b secrethandoff.KeyBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	if err := secrethandoff.VerifyBundle(&b, ed25519.PublicKey(k.OperatorPub)); err != nil {
		return nil, fmt.Errorf("%s: %w — tell the operator", path, err)
	}
	// Rollback guard: an operator signature never expires, so after a key
	// rotation the OLD bundle still verifies — and a compromised server can
	// keep serving it, directing senders to the leaked key. Pin the newest
	// created_at ever accepted per actor and refuse anything older.
	if err := checkBundlePin(&b); err != nil {
		return nil, err
	}
	return &b, nil
}

// bundle pins live in ~/.board-secret/bundle_pins.json: actor -> unix time
// of the newest accepted bundle.
func pinsPath() string { return filepath.Join(keysDir(), "bundle_pins.json") }

func checkBundlePin(b *secrethandoff.KeyBundle) error {
	pins := map[string]int64{}
	if data, err := os.ReadFile(pinsPath()); err == nil {
		_ = json.Unmarshal(data, &pins)
	}
	seen := b.CreatedAt.UTC().Unix()
	if pinned, ok := pins[b.Actor]; ok && seen < pinned {
		return fmt.Errorf("bundle for %q is older (created %s) than one previously accepted on this host — possible rollback to a rotated key; refusing. If the rotation is legitimate, the operator must sign a NEWER bundle", b.Actor, b.CreatedAt)
	}
	if pinned, ok := pins[b.Actor]; !ok || seen > pinned {
		pins[b.Actor] = seen
		if err := os.MkdirAll(keysDir(), 0o700); err != nil {
			return err
		}
		data, _ := json.Marshal(pins)
		if err := os.WriteFile(pinsPath(), data, 0o600); err != nil {
			return fmt.Errorf("cannot persist bundle pin: %w — refusing to proceed without rollback protection", err)
		}
	}
	return nil
}

// confineToSecretsDir resolves outPath strictly inside ~/.secrets: no "..",
// no symlinked escape, absolute paths only if already under the jail.
func confineToSecretsDir(outPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	jail := filepath.Join(home, ".secrets")

	p := outPath
	if !filepath.IsAbs(p) {
		p = filepath.Join(jail, p)
	}
	p = filepath.Clean(p)
	if p != jail && !strings.HasPrefix(p, jail+string(filepath.Separator)) {
		return "", fmt.Errorf("--out must stay under %s (got %s)", jail, outPath)
	}
	// Refuse a symlink at the destination, any symlinked parent inside the
	// jail, or the jail itself: a link pointing outside would defeat the
	// confinement.
	if fi, err := os.Lstat(jail); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s itself is a symlink; refusing", jail)
	}
	for probe := p; probe != jail && strings.HasPrefix(probe, jail); probe = filepath.Dir(probe) {
		if fi, err := os.Lstat(probe); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s is a symlink; refusing", probe)
		}
	}
	return p, nil
}

// checkSeenList / markSeen keep a local record of accepted secret_ids so a
// replayed envelope is refused even though its signature verifies.
func seenPath() string { return filepath.Join(keysDir(), "seen_ids") }

func checkSeenList(id string) error {
	data, err := os.ReadFile(seenPath())
	if err != nil {
		return nil // no list yet
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == id {
			return fmt.Errorf("secret_id %s was already accepted on this host — replay refused", id)
		}
	}
	return nil
}

func markSeen(id string) error {
	if err := os.MkdirAll(keysDir(), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(seenPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, id); err != nil {
		f.Close()
		return err
	}
	// fsync before the secret is written: replay protection must survive a
	// crash between "accepted" and "stored".
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// --- tiny flag helpers (stdlib flag fights subcommands) -------------------

func flagValue(args []string, name, def string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v
		}
	}
	return def
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func lastPositional(args []string) string {
	skip := false
	last := ""
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if strings.HasPrefix(a, "--") {
			if !strings.Contains(a, "=") {
				skip = true
			}
			continue
		}
		last = a
	}
	return last
}
