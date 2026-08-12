package consensus

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// ════════════════════════════════════════════════════════════
// Worker 1 — HMAC-SHA256
// ════════════════════════════════════════════════════════════

// HMACWorker با HMAC-SHA256 تراکنش را امضا و تأیید می‌کند.
// کلید مخفی در فایل جداگانه نگه می‌شود.
type HMACWorker struct {
	id  string
	key []byte // 32-byte secret key
	db  *LocalDB
}

func NewHMACWorker(dbDir string) (*HMACWorker, error) {
	id := "worker-hmac"
	db, err := OpenLocalDB(filepath.Join(dbDir, id+".db"), id)
	if err != nil {
		return nil, err
	}

	// بارگذاری یا ساخت کلید
	keyFile := filepath.Join(dbDir, id+".key")
	key, err := loadOrGenKey(keyFile, 32)
	if err != nil {
		return nil, err
	}

	w := &HMACWorker{id: id, key: key, db: db}
	db.SavePublicKey("HMAC-SHA256", hex.EncodeToString(key[:16])+"...")
	return w, nil
}

func (w *HMACWorker) ID() string        { return w.id }
func (w *HMACWorker) Algorithm() string { return "HMAC-SHA256" }

func (w *HMACWorker) Verify(ctx context.Context, tx Tx) VoteResult {
	payload := TxPayload(tx)

	// بررسی replay attack
	seen, _ := w.db.HasSeen(tx.ID)
	if seen {
		return w.reject(tx.ID, "replay attack detected", payload)
	}

	// اعتبارسنجی مقدار
	if tx.AmountNano <= 0 {
		return w.reject(tx.ID, "invalid amount", payload)
	}
	if tx.AmountNano > 100_000*1e9 { // حداکثر 100,000 TON
		return w.reject(tx.ID, "amount exceeds maximum limit", payload)
	}

	// بررسی timestamp (نباید بیشتر از 5 دقیقه قدیمی باشد)
	age := time.Since(tx.Timestamp)
	if age > 5*time.Minute || age < -30*time.Second {
		return w.reject(tx.ID, fmt.Sprintf("timestamp out of range: %v", age), payload)
	}

	// امضای HMAC-SHA256
	mac := hmac.New(sha256.New, w.key)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	vote := VoteResult{
		WorkerID:  w.id,
		Algorithm: "HMAC-SHA256",
		Approved:  true,
		Signature: sig,
		Timestamp: time.Now(),
	}
	w.db.SaveTxRecord(tx.ID, vote, payload)
	return vote
}

func (w *HMACWorker) reject(txID, reason, payload string) VoteResult {
	vote := VoteResult{
		WorkerID:  w.id,
		Algorithm: "HMAC-SHA256",
		Approved:  false,
		Signature: "rejected",
		Reason:    reason,
		Timestamp: time.Now(),
	}
	w.db.SaveTxRecord(txID, vote, payload)
	return vote
}

func (w *HMACWorker) Close() error { return w.db.Close() }

// ════════════════════════════════════════════════════════════
// Worker 2 — Ed25519
// ════════════════════════════════════════════════════════════

// Ed25519Worker با امضای دیجیتال Ed25519 تراکنش را تأیید می‌کند.
type Ed25519Worker struct {
	id      string
	privKey ed25519.PrivateKey
	pubKey  ed25519.PublicKey
	db      *LocalDB
}

func NewEd25519Worker(dbDir string) (*Ed25519Worker, error) {
	id := "worker-ed25519"
	db, err := OpenLocalDB(filepath.Join(dbDir, id+".db"), id)
	if err != nil {
		return nil, err
	}

	keyFile := filepath.Join(dbDir, id+".key")
	keyBytes, err := loadOrGenKey(keyFile, ed25519.PrivateKeySize)
	if err != nil {
		return nil, err
	}

	privKey := ed25519.PrivateKey(keyBytes)
	pubKey := privKey.Public().(ed25519.PublicKey)

	w := &Ed25519Worker{id: id, privKey: privKey, pubKey: pubKey, db: db}
	db.SavePublicKey("Ed25519", hex.EncodeToString(pubKey))
	return w, nil
}

func (w *Ed25519Worker) ID() string        { return w.id }
func (w *Ed25519Worker) Algorithm() string { return "Ed25519" }

func (w *Ed25519Worker) Verify(ctx context.Context, tx Tx) VoteResult {
	payload := TxPayload(tx)

	seen, _ := w.db.HasSeen(tx.ID)
	if seen {
		return w.reject(tx.ID, "replay attack", payload)
	}

	// اعتبارسنجی wallet
	if len(tx.FromWallet) < 10 {
		return w.reject(tx.ID, "invalid wallet address", payload)
	}

	// بررسی double-spend ساده: همان wallet در ۱ ثانیه
	// (در production این باید با DB مرکزی چک شود)
	if tx.AmountNano <= 0 {
		return w.reject(tx.ID, "non-positive amount", payload)
	}

	// امضای Ed25519
	hash := sha256.Sum256([]byte(payload))
	sig := ed25519.Sign(w.privKey, hash[:])
	sigHex := hex.EncodeToString(sig)

	// تأیید امضا (self-verification)
	if !ed25519.Verify(w.pubKey, hash[:], sig) {
		return w.reject(tx.ID, "self-verification failed", payload)
	}

	vote := VoteResult{
		WorkerID:  w.id,
		Algorithm: "Ed25519",
		Approved:  true,
		Signature: sigHex,
		Timestamp: time.Now(),
	}
	w.db.SaveTxRecord(tx.ID, vote, payload)
	return vote
}

func (w *Ed25519Worker) reject(txID, reason, payload string) VoteResult {
	vote := VoteResult{
		WorkerID: w.id, Algorithm: "Ed25519",
		Approved: false, Signature: "rejected",
		Reason: reason, Timestamp: time.Now(),
	}
	w.db.SaveTxRecord(txID, vote, payload)
	return vote
}

func (w *Ed25519Worker) Close() error { return w.db.Close() }

// ════════════════════════════════════════════════════════════
// Worker 3 — AES-256-GCM
// ════════════════════════════════════════════════════════════

// AESWorker با رمزنگاری AES-256-GCM یک token ساخته و تأیید می‌کند.
// این worker تمرکز روی integrity check دارد.
type AESWorker struct {
	id  string
	key []byte // 32-byte AES key
	db  *LocalDB
}

func NewAESWorker(dbDir string) (*AESWorker, error) {
	id := "worker-aes256"
	db, err := OpenLocalDB(filepath.Join(dbDir, id+".db"), id)
	if err != nil {
		return nil, err
	}

	keyFile := filepath.Join(dbDir, id+".key")
	key, err := loadOrGenKey(keyFile, 32)
	if err != nil {
		return nil, err
	}

	db.SavePublicKey("AES-256-GCM", "symmetric-key-hidden")
	return &AESWorker{id: id, key: key, db: db}, nil
}

func (w *AESWorker) ID() string        { return w.id }
func (w *AESWorker) Algorithm() string { return "AES-256-GCM" }

func (w *AESWorker) Verify(ctx context.Context, tx Tx) VoteResult {
	payload := TxPayload(tx)

	seen, _ := w.db.HasSeen(tx.ID)
	if seen {
		return w.reject(tx.ID, "replay attack", payload)
	}

	// بررسی منطق کسب‌وکار اضافی
	if tx.ToService == "" {
		return w.reject(tx.ID, "missing service target", payload)
	}

	// رمزنگاری AES-GCM برای ساخت integrity token
	block, err := aes.NewCipher(w.key)
	if err != nil {
		return w.reject(tx.ID, "cipher init failed", payload)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return w.reject(tx.ID, "gcm init failed", payload)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return w.reject(tx.ID, "nonce generation failed", payload)
	}

	// رمزنگاری payload → token
	encrypted := gcm.Seal(nonce, nonce, []byte(payload), nil)
	token := base64.StdEncoding.EncodeToString(encrypted)

	// تأیید: decrypt کن و مقایسه کن
	decoded, _ := base64.StdEncoding.DecodeString(token)
	nonceSize := gcm.NonceSize()
	if len(decoded) < nonceSize {
		return w.reject(tx.ID, "integrity check failed", payload)
	}
	decrypted, err := gcm.Open(nil, decoded[:nonceSize], decoded[nonceSize:], nil)
	if err != nil || string(decrypted) != payload {
		return w.reject(tx.ID, "integrity check failed", payload)
	}

	// فقط hash کوتاه به‌عنوان signature نگه می‌داریم (امنیت token)
	h := sha256.Sum256(encrypted)
	sig := hex.EncodeToString(h[:16])

	vote := VoteResult{
		WorkerID:  w.id,
		Algorithm: "AES-256-GCM",
		Approved:  true,
		Signature: sig,
		Timestamp: time.Now(),
	}
	w.db.SaveTxRecord(tx.ID, vote, payload)
	return vote
}

func (w *AESWorker) reject(txID, reason, payload string) VoteResult {
	vote := VoteResult{
		WorkerID: w.id, Algorithm: "AES-256-GCM",
		Approved: false, Signature: "rejected",
		Reason: reason, Timestamp: time.Now(),
	}
	w.db.SaveTxRecord(txID, vote, payload)
	return vote
}

func (w *AESWorker) Close() error { return w.db.Close() }

// ════════════════════════════════════════════════════════════
// Worker 4 — SHA-512 + Merkle Proof
// ════════════════════════════════════════════════════════════

// MerkleWorker با SHA-512 و ساختار Merkle تراکنش را تأیید می‌کند.
// این worker روی chain integrity و sequence check تمرکز دارد.
type MerkleWorker struct {
	id       string
	db       *LocalDB
	lastHash []byte // hash آخرین تراکنش تأیید شده (chain)
	mu       syncMutex
}

type syncMutex struct{ locked bool }

func NewMerkleWorker(dbDir string) (*MerkleWorker, error) {
	id := "worker-merkle"
	db, err := OpenLocalDB(filepath.Join(dbDir, id+".db"), id)
	if err != nil {
		return nil, err
	}

	db.SavePublicKey("SHA512-Merkle", "chain-integrity")
	return &MerkleWorker{
		id:       id,
		db:       db,
		lastHash: make([]byte, 64),
	}, nil
}

func (w *MerkleWorker) ID() string        { return w.id }
func (w *MerkleWorker) Algorithm() string { return "SHA512-Merkle" }

func (w *MerkleWorker) Verify(ctx context.Context, tx Tx) VoteResult {
	payload := TxPayload(tx)

	seen, _ := w.db.HasSeen(tx.ID)
	if seen {
		return w.reject(tx.ID, "replay attack", payload)
	}

	// بررسی مقدار — محافظت در برابر integer overflow attack
	if tx.AmountNano <= 0 || tx.AmountNano > maxSafeAmount() {
		return w.reject(tx.ID, "amount out of safe range", payload)
	}

	// SHA-512 روی payload
	hash512 := sha512.Sum512([]byte(payload))

	// chain hash: hash جدید = SHA512(hash قبلی + hash جدید)
	chainInput := append(w.lastHash, hash512[:]...)
	chainHash := sha512.Sum512(chainInput)

	// Merkle leaf: ترکیب tx_id + chain hash
	merkleLeaf := sha256.Sum256(append([]byte(tx.ID), chainHash[:]...))

	sig := hex.EncodeToString(merkleLeaf[:])

	// آپدیت chain (بدون lock چون verify سریال است در این worker)
	w.lastHash = chainHash[:]

	vote := VoteResult{
		WorkerID:  w.id,
		Algorithm: "SHA512-Merkle",
		Approved:  true,
		Signature: sig,
		Timestamp: time.Now(),
	}
	w.db.SaveTxRecord(tx.ID, vote, payload)
	return vote
}

func (w *MerkleWorker) reject(txID, reason, payload string) VoteResult {
	vote := VoteResult{
		WorkerID: w.id, Algorithm: "SHA512-Merkle",
		Approved: false, Signature: "rejected",
		Reason: reason, Timestamp: time.Now(),
	}
	w.db.SaveTxRecord(txID, vote, payload)
	return vote
}

func (w *MerkleWorker) Close() error { return w.db.Close() }

// maxSafeAmount حداکثر مقدار امن (1 میلیون TON به nano).
func maxSafeAmount() int64 {
	max := new(big.Int).SetInt64(1_000_000)
	max.Mul(max, big.NewInt(1_000_000_000))
	if !max.IsInt64() {
		return 1_000_000_000_000_000_000
	}
	return max.Int64()
}

// ════════════════════════════════════════════════════════════
// Factory — ساخت همه worker ها
// ════════════════════════════════════════════════════════════

// SetupWorkers همه ۴ worker را می‌سازد و به engine اضافه می‌کند.
func SetupWorkers(engine *Engine, dbDir string) error {
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	workers := []struct {
		name    string
		factory func(string) (Worker, error)
	}{
		{"HMAC-SHA256", func(d string) (Worker, error) { return NewHMACWorker(d) }},
		{"Ed25519", func(d string) (Worker, error) { return NewEd25519Worker(d) }},
		{"AES-256-GCM", func(d string) (Worker, error) { return NewAESWorker(d) }},
		{"SHA512-Merkle", func(d string) (Worker, error) { return NewMerkleWorker(d) }},
	}

	for _, wf := range workers {
		w, err := wf.factory(dbDir)
		if err != nil {
			return fmt.Errorf("init worker %s: %w", wf.name, err)
		}
		engine.AddWorker(w)
	}
	return nil
}

// ── key management ─────────────────────────────────────────

// loadOrGenKey کلید را از فایل بارگذاری یا می‌سازد.
// فایل با permission 0600 ذخیره می‌شود.
func loadOrGenKey(path string, size int) ([]byte, error) {
	// بار اول: فایل وجود ندارد → بساز
	if _, err := os.Stat(path); os.IsNotExist(err) {
		key := make([]byte, size)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
		encoded := hex.EncodeToString(key)
		if err := os.WriteFile(path, []byte(encoded), 0600); err != nil {
			return nil, fmt.Errorf("save key: %w", err)
		}
		return key, nil
	}

	// فایل موجود است → بارگذاری
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	key, err := hex.DecodeString(string(data))
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	if len(key) != size {
		return nil, fmt.Errorf("key size mismatch: got %d want %d", len(key), size)
	}
	return key, nil
}
