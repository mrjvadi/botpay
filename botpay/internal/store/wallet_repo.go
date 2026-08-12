package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PayHandle کامنت شخصی و یکتای کاربر برای واریز روی ولت مشترک.
// از telegram_id (که یکتا است) مشتق می‌شود تا یکتایی تضمین شود و هیچ‌گاه
// با pay_handle خالی برخورد unique-constraint رخ ندهد.
func PayHandle(telegramID int64) string {
	return "u_" + strconv.FormatInt(telegramID, 36)
}

// GetOrCreateWallet کیف پول کاربر را پیدا یا می‌سازد.
// همه کاربرها یک masterAddr مشترک دارند — فقط telegram_id یکتا است و هر
// کیف پول یک کامنت شخصی یکتا (pay_handle) دریافت می‌کند.
func (s *Store) GetOrCreateWallet(ctx context.Context, telegramID int64, tonAddress string) (*Wallet, error) {
	handle := PayHandle(telegramID)
	var w Wallet
	result := s.db.WithContext(ctx).
		Where(Wallet{TelegramID: telegramID}).
		Attrs(Wallet{TONAddress: tonAddress, IsActive: true, PayHandle: handle}).
		FirstOrCreate(&w)
	if result.Error != nil {
		return &w, result.Error
	}
	// backfill برای کیف‌پول‌های قدیمی که با pay_handle خالی ساخته شده بودند.
	if w.PayHandle == "" {
		if err := s.db.WithContext(ctx).Model(&w).Update("pay_handle", handle).Error; err == nil {
			w.PayHandle = handle
		}
	}
	return &w, nil
}

// GetWalletByPayHandle کیف پول را بر اساس کامنت شخصی پیدا می‌کند.
func (s *Store) GetWalletByPayHandle(ctx context.Context, handle string) (*Wallet, error) {
	var w Wallet
	err := s.db.WithContext(ctx).Where("pay_handle = ?", handle).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &w, err
}

// SetWalletLang زبان رابط کاربر را ذخیره می‌کند.
func (s *Store) SetWalletLang(ctx context.Context, telegramID int64, lang string) error {
	return s.db.WithContext(ctx).Model(&Wallet{}).
		Where("telegram_id = ?", telegramID).
		Update("lang", lang).Error
}

func (s *Store) GetWallet(ctx context.Context, telegramID int64) (*Wallet, error) {
	var w Wallet
	err := s.db.WithContext(ctx).Where("telegram_id = ?", telegramID).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &w, err
}

func (s *Store) GetWalletByID(ctx context.Context, id uuid.UUID) (*Wallet, error) {
	var w Wallet
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &w, err
}

func (s *Store) GetWalletByAddress(ctx context.Context, address string) (*Wallet, error) {
	var w Wallet
	err := s.db.WithContext(ctx).Where("ton_address = ?", address).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &w, err
}

// Deposit واریز TON به wallet — atomic با transaction.
// DepositRecord داده‌ی کاملِ یک واریزِ on-chain TON — تمام اطلاعاتی که از شبکه
// می‌گیریم ذخیره می‌شود (آدرس‌ها، کامنت، logical time، unix time، کارمزد).
type DepositRecord struct {
	WalletID   uuid.UUID
	AmountNano int64
	FeeNano    int64
	TxHash     string
	FromAddr   string
	ToAddr     string
	Comment    string // in_msg comment (کدِ فاکتور)
	LT         int64  // logical time تراکنش TON
	Utime      int64  // unix time on-chain
	Desc       string
}

// Deposit واریزِ TON را ثبت می‌کند: افزایش ton_balance، یک Transactionِ کامل با
// همه‌ی اطلاعاتِ on-chain، و یک بلوکِ credit در دفترِ هش. با tx_hash idempotent است.
func (s *Store) Deposit(ctx context.Context, rec DepositRecord) (*Transaction, error) {
	var tx *Transaction
	err := s.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		var existing Transaction
		if err := db.Where("tx_hash = ?", rec.TxHash).First(&existing).Error; err == nil {
			tx = &existing
			return nil // تکراری — ok
		}
		var w Wallet
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", rec.WalletID).First(&w).Error; err != nil {
			return err
		}
		if err := db.Model(&Wallet{}).Where("id = ?", rec.WalletID).
			UpdateColumn("ton_balance", gorm.Expr("ton_balance + ?", rec.AmountNano)).Error; err != nil {
			return err
		}
		now := time.Now()
		tx = &Transaction{
			WalletID:    rec.WalletID,
			Type:        TxDeposit,
			Status:      TxConfirmed,
			Amount:      rec.AmountNano,
			Fee:         rec.FeeNano,
			TxHash:      rec.TxHash,
			FromAddress: rec.FromAddr,
			ToAddress:   rec.ToAddr,
			Ref:         rec.Comment,
			TxLT:        rec.LT,
			TxUtime:     rec.Utime,
			Description: rec.Desc,
			ConfirmedAt: &now,
		}
		if err := db.Create(tx).Error; err != nil {
			return err
		}
		balanceAfter := w.TONBalance + w.Credit + rec.AmountNano
		return s.appendLedger(db, rec.WalletID, tx.ID, EntryCredit, rec.AmountNano, balanceAfter, rec.Comment, rec.Desc)
	})
	return tx, err
}

// Deduct کسر از موجودی — ابتدا از اعتبار، سپس TON.
// amountNano باید مثبت باشد (chi چک هم در payresponder انجام می‌شود، ولی این
// یک لایه‌ی دوم دفاعی است تا فراخوانی مستقیم آینده هم اشتباهاً مبلغ منفی را
// نپذیرد — منفی یعنی "کسر" در واقع اعتبار اضافه می‌کند، دقیقاً برعکسِ مقصود).
// serviceID+ref با هم idempotency key هستند: اگر تراکنشی از قبل با همین جفت
// ثبت شده باشد (مثلاً به‌خاطر retry سمت کلاینت روی timeout)، دوباره کسر
// نمی‌شود — همان تراکنش قبلی برگردانده می‌شود.
func (s *Store) Deduct(ctx context.Context, walletID uuid.UUID, amountNano int64, serviceID, ref, desc string) (*Transaction, bool, error) {
	if amountNano <= 0 {
		return nil, false, fmt.Errorf("invalid amount: must be positive")
	}
	var tx *Transaction
	created := false
	err := s.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		if ref != "" {
			var existing Transaction
			err := db.Where("wallet_id = ? AND service_id = ? AND ref = ? AND type = ?",
				walletID, serviceID, ref, TxPayment).First(&existing).Error
			if err == nil {
				tx = &existing // تکراری — idempotent no-op، دوباره کسر نمی‌شود
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		var w Wallet
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", walletID).First(&w).Error; err != nil {
			return err
		}

		if !w.HasEnough(amountNano) {
			return fmt.Errorf("insufficient balance")
		}

		// ابتدا از اعتبار کم کن
		var creditUsed, tonUsed int64
		if w.Credit >= amountNano {
			creditUsed = amountNano
		} else {
			creditUsed = w.Credit
			tonUsed = amountNano - creditUsed
		}

		updates := map[string]any{}
		if creditUsed > 0 {
			updates["credit"] = gorm.Expr("credit - ?", creditUsed)
		}
		if tonUsed > 0 {
			updates["ton_balance"] = gorm.Expr("ton_balance - ?", tonUsed)
		}

		if err := db.Model(&Wallet{}).Where("id = ?", walletID).
			Updates(updates).Error; err != nil {
			return err
		}

		now := time.Now()
		tx = &Transaction{
			WalletID:    walletID,
			Type:        TxPayment,
			Status:      TxConfirmed,
			Amount:      -amountNano,
			ServiceID:   serviceID,
			Ref:         ref,
			TxHash:      "int-" + uuid.New().String(),
			Description: desc,
			ConfirmedAt: &now,
		}
		if err := db.Create(tx).Error; err != nil {
			return err
		}
		balanceAfter := (w.TONBalance + w.Credit) - amountNano
		if err := s.appendLedger(db, walletID, tx.ID, EntryDebit, amountNano, balanceAfter, ref, desc); err != nil {
			return err
		}
		created = true
		return nil
	})
	return tx, created, err
}

// AddCredit افزایش اعتبار داخلی را با کلید serviceID+ref به‌صورت idempotent انجام می‌دهد.
// ref برای تمام callerهای سرویس‌به‌سرویس اجباری است؛ retry همان تراکنش قبلی را برمی‌گرداند.
// created=true یعنی یک credit تازه اعمال شد؛ created=false یعنی این فراخوانی یک
// retry idempotent بود (تراکنش قبلی برگردانده شد و موجودی تغییر نکرد). caller از
// این برای ارسالِ دقیقاً-یک‌بارِ اعلان به کاربر استفاده می‌کند.
func (s *Store) AddCredit(ctx context.Context, walletID uuid.UUID, amountNano int64, serviceID, ref, desc, metadata string) (*Transaction, bool, error) {
	if amountNano <= 0 {
		return nil, false, fmt.Errorf("invalid amount: must be positive")
	}
	if serviceID == "" || ref == "" {
		return nil, false, fmt.Errorf("service_id and ref are required")
	}
	var tx *Transaction
	created := false
	err := s.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		// قفل wallet همه creditهای هم‌زمان همین کاربر را serialize می‌کند؛ constraint DB
		// نیز از تکرار در برابر مسیرهای آینده دفاع می‌کند.
		var wallet Wallet
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", walletID).First(&wallet).Error; err != nil {
			return err
		}
		var existing Transaction
		err := db.Where("wallet_id = ? AND service_id = ? AND ref = ? AND type = ?",
			walletID, serviceID, ref, TxCreditAdd).First(&existing).Error
		if err == nil {
			tx = &existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := db.Model(&Wallet{}).Where("id = ?", walletID).
			UpdateColumn("credit", gorm.Expr("credit + ?", amountNano)).Error; err != nil {
			return err
		}
		now := time.Now()
		tx = &Transaction{
			WalletID: walletID, Type: TxCreditAdd, Status: TxConfirmed, Amount: amountNano,
			ServiceID: serviceID, Ref: ref, Metadata: metadata,
			TxHash: "int-" + uuid.New().String(), Description: desc, ConfirmedAt: &now,
		}
		if err := db.Create(tx).Error; err != nil {
			return err
		}
		balanceAfter := wallet.TONBalance + wallet.Credit + amountNano
		if err := s.appendLedger(db, walletID, tx.ID, EntryCredit, amountNano, balanceAfter, ref, desc); err != nil {
			return err
		}
		created = true
		return nil
	})
	return tx, created, err
}

// ListTransactions تراکنش‌های اخیر یک کیف پول را برمی‌گرداند.
func (s *Store) ListTransactions(ctx context.Context, walletID uuid.UUID, limit int) ([]Transaction, error) {
	var txs []Transaction
	err := s.db.WithContext(ctx).
		Where("wallet_id = ?", walletID).
		Order("created_at DESC").
		Limit(limit).
		Find(&txs).Error
	return txs, err
}
