package consensus

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// LocalDB یک SQLite database محلی برای هر worker.
// هر worker record مستقل از تراکنش‌هایی که verify کرده نگه می‌دارد.
// این isolation تضمین می‌کند که compromise یک worker روی بقیه تأثیر نمی‌گذارد.
type LocalDB struct {
	db       *sql.DB
	workerID string
}

// OpenLocalDB یک LocalDB باز یا می‌سازد.
func OpenLocalDB(path, workerID string) (*LocalDB, error) {
	db, err := sql.Open("sqlite", path+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}

	ldb := &LocalDB{db: db, workerID: workerID}
	if err := ldb.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return ldb, nil
}

func (l *LocalDB) migrate() error {
	_, err := l.db.Exec(`
		CREATE TABLE IF NOT EXISTS tx_records (
			id          TEXT PRIMARY KEY,
			tx_id       TEXT NOT NULL,
			worker_id   TEXT NOT NULL,
			algorithm   TEXT NOT NULL,
			approved    INTEGER NOT NULL,
			signature   TEXT NOT NULL,
			payload     TEXT NOT NULL,
			reason      TEXT,
			created_at  TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_tx_id ON tx_records(tx_id);
		CREATE INDEX IF NOT EXISTS idx_created ON tx_records(created_at);

		CREATE TABLE IF NOT EXISTS worker_keys (
			id          TEXT PRIMARY KEY,
			worker_id   TEXT NOT NULL UNIQUE,
			algorithm   TEXT NOT NULL,
			public_key  TEXT NOT NULL,
			created_at  TEXT NOT NULL
		);
	`)
	return err
}

// SaveRecord یک نتیجه verify را ذخیره می‌کند.
func (l *LocalDB) SaveRecord(vote VoteResult, payload string) error {
	id := vote.WorkerID + "-" + vote.WorkerID[:4] + fmt.Sprintf("%d", time.Now().UnixNano())
	_, err := l.db.Exec(`
		INSERT OR IGNORE INTO tx_records
		(id, tx_id, worker_id, algorithm, approved, signature, payload, reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		strings_before_dash(vote.Signature), // tx_id از signature استخراج نمی‌شه — فقط worker_id
		vote.WorkerID,
		vote.Algorithm,
		boolToInt(vote.Approved),
		vote.Signature,
		payload,
		vote.Reason,
		vote.Timestamp.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// SaveTxRecord با tx_id صریح.
func (l *LocalDB) SaveTxRecord(txID string, vote VoteResult, payload string) error {
	_, err := l.db.Exec(`
		INSERT OR IGNORE INTO tx_records
		(id, tx_id, worker_id, algorithm, approved, signature, payload, reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		vote.WorkerID+"-"+txID,
		txID,
		vote.WorkerID,
		vote.Algorithm,
		boolToInt(vote.Approved),
		vote.Signature,
		payload,
		vote.Reason,
		vote.Timestamp.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// FindByTxID سوابق یک تراکنش را برمی‌گرداند.
func (l *LocalDB) FindByTxID(txID string) ([]TxRecord, error) {
	rows, err := l.db.Query(`
		SELECT tx_id, worker_id, algorithm, approved, signature, reason, created_at
		FROM tx_records WHERE tx_id = ?`, txID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []TxRecord
	for rows.Next() {
		var r TxRecord
		var approved int
		var ts string
		if err := rows.Scan(&r.TxID, &r.WorkerID, &r.Algorithm, &approved, &r.Signature, &r.Reason, &ts); err != nil {
			continue
		}
		r.Approved = approved == 1
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
		records = append(records, r)
	}
	return records, nil
}

// HasSeen بررسی می‌کند آیا این تراکنش قبلاً پردازش شده — جلوگیری از replay.
func (l *LocalDB) HasSeen(txID string) (bool, error) {
	if txID == "" {
		return false, nil
	}
	var count int
	err := l.db.QueryRow(
		`SELECT COUNT(*) FROM tx_records WHERE tx_id = ? AND worker_id = ?`,
		txID, l.workerID,
	).Scan(&count)
	return count > 0, err
}

// SavePublicKey کلید عمومی worker را ذخیره می‌کند.
func (l *LocalDB) SavePublicKey(algo, pubKey string) error {
	_, err := l.db.Exec(`
		INSERT OR REPLACE INTO worker_keys (id, worker_id, algorithm, public_key, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		l.workerID+"-key",
		l.workerID, algo, pubKey,
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// Stats آمار worker.
func (l *LocalDB) Stats() (total, approved, rejected int64, err error) {
	err = l.db.QueryRow(`
		SELECT COUNT(*), SUM(CASE WHEN approved=1 THEN 1 ELSE 0 END),
		       SUM(CASE WHEN approved=0 THEN 1 ELSE 0 END)
		FROM tx_records WHERE worker_id = ?`, l.workerID).
		Scan(&total, &approved, &rejected)
	return
}

func (l *LocalDB) Close() error { return l.db.Close() }

// TxRecord رکورد تراکنش در local DB.
type TxRecord struct {
	TxID      string
	WorkerID  string
	Algorithm string
	Approved  bool
	Signature string
	Reason    string
	CreatedAt time.Time
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func strings_before_dash(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}
