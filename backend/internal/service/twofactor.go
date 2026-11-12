package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"time"

	"ohvat/backend/internal/domain"
)

// ===== 2FA сотрудников: TOTP + резервные коды =====

// TwoFactorRepository — секрет TOTP, резервные коды, журнал.
type TwoFactorRepository interface {
	TOTP(ctx context.Context, userID int64) (domain.TOTP, error)
	// SaveTOTPSecret — новый секрет; 2FA уже подтверждена — domain.ErrConflict.
	SaveTOTPSecret(ctx context.Context, userID int64, secret []byte) error
	// AcceptTOTPStep — принять шаг (больше последнего, атомарно); false — повтор кода.
	AcceptTOTPStep(ctx context.Context, userID, step int64, confirming bool, now time.Time) (bool, error)
	// TOTPFailure — неверный код; true — достигнут предел и 2FA заблокирована до lockUntil.
	TOTPFailure(ctx context.Context, userID int64, maxFailures int, lockUntil time.Time) (bool, error)
	ReplaceBackupCodes(ctx context.Context, userID int64, hashes [][]byte, e domain.AuditEntry) error
	UseBackupCode(ctx context.Context, userID int64, hash []byte, now time.Time) (bool, error)
	BackupCodesLeft(ctx context.Context, userID int64) (int, error)
	ResetTwoFactor(ctx context.Context, userID int64, e domain.AuditEntry) error
	Audit(ctx context.Context, e domain.AuditEntry) error
}

// SessionMFARepository — подтверждение 2FA в сессии (по хешу токена).
type SessionMFARepository interface {
	SessionMFA(ctx context.Context, tokenHash []byte, now time.Time) (*time.Time, error)
	SetSessionMFA(ctx context.Context, tokenHash []byte, at, now time.Time) error
}

// QRCoder — картинка QR-кода (PNG) для ссылки otpauth://.
type QRCoder interface {
	PNG(text string) ([]byte, error)
}

// TwoFactorConfig — правила 2FA.
type TwoFactorConfig struct {
	Issuer      string        // название в приложении-аутентификаторе, «Охват»
	MaxAge      time.Duration // подтверждение в сессии действует, 12 ч
	MaxFailures int           // неверных кодов подряд до блокировки, 5
	LockFor     time.Duration // блокировка после перебора, 15 мин
	BackupCodes int           // резервных кодов, 10
	Secret      []byte        // ключ HMAC для хешей резервных кодов (AUTH_SECRET)
}

func (c *TwoFactorConfig) defaults() {
	if c.Issuer == "" {
		c.Issuer = "Охват"
	}
	if c.MaxAge <= 0 {
		c.MaxAge = 12 * time.Hour
	}
	if c.MaxFailures <= 0 {
		c.MaxFailures = 5
	}
	if c.LockFor <= 0 {
		c.LockFor = 15 * time.Minute
	}
	if c.BackupCodes <= 0 {
		c.BackupCodes = 10
	}
}

// TwoFactorDeps — зависимости 2FA.
type TwoFactorDeps struct {
	Repo     TwoFactorRepository
	Sessions SessionMFARepository
	Cipher   TokenCipher // шифрование секрета TOTP (AES-256-GCM)
	QR       QRCoder     // nil — без картинки (только ссылка и ключ)
	Clock    Clock
	Random   io.Reader
	Log      *slog.Logger
}

// TwoFactor — 2FA для админки: настройка (секрет → QR → первый код → резервные коды), подтверждение в сессии на
// 12 часов, резервные коды, сброс администратором. Защита: секрет зашифрован, повтор кода не проходит, перебор —
// блокировка, сравнение — за постоянное время, резервные коды — только HMAC.
type TwoFactor struct {
	d   TwoFactorDeps
	cfg TwoFactorConfig
}

// NewTwoFactor создаёт сервис 2FA.
func NewTwoFactor(d TwoFactorDeps, cfg TwoFactorConfig) *TwoFactor {
	cfg.defaults()
	if d.Clock == nil {
		d.Clock = SystemClock{}
	}
	if d.Random == nil {
		d.Random = cryptoRand
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if len(cfg.Secret) == 0 {
		cfg.Secret = make([]byte, 32)
		_, _ = io.ReadFull(d.Random, cfg.Secret)
	}
	return &TwoFactor{d: d, cfg: cfg}
}

// TwoFactorStatus — состояние 2FA сотрудника и текущей сессии.
type TwoFactorStatus struct {
	Enabled       bool       // 2FA настроена
	Verified      bool       // подтверждена в этой сессии не раньше MaxAge назад
	VerifiedUntil *time.Time // до когда действует подтверждение
	BackupLeft    int        // неиспользованных резервных кодов
}

// TwoFactorSetup — данные для приложения-аутентификатора.
type TwoFactorSetup struct {
	Secret string // ключ base32 (ввести вручную)
	URI    string // otpauth://…
	QR     []byte // PNG; nil — нет
}

func totpAAD(userID int64) []byte { return []byte("totp:" + strconv.FormatInt(userID, 10)) }

// Status — настроена ли 2FA и подтверждена ли в этой сессии.
func (s *TwoFactor) Status(ctx context.Context, u domain.User, token string) (TwoFactorStatus, error) {
	if !u.Staff() {
		return TwoFactorStatus{}, ErrNotStaff
	}
	var st TwoFactorStatus
	t, err := s.d.Repo.TOTP(ctx, u.ID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return st, nil
	case err != nil:
		return st, err
	}
	st.Enabled = t.ConfirmedAt != nil
	if !st.Enabled {
		return st, nil
	}
	if st.BackupLeft, err = s.d.Repo.BackupCodesLeft(ctx, u.ID); err != nil {
		return st, err
	}
	at, err := s.d.Sessions.SessionMFA(ctx, hashToken(token), s.d.Clock.Now())
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return st, err
	}
	if at != nil {
		until := at.Add(s.cfg.MaxAge)
		if until.After(s.d.Clock.Now()) {
			st.Verified, st.VerifiedUntil = true, &until
		}
	}
	return st, nil
}

// Require — доступ к админке: 2FA настроена (иначе ErrTwoFactorSetup) и подтверждена в сессии не раньше 12 часов
// назад (иначе ErrTwoFactorRequired).
func (s *TwoFactor) Require(ctx context.Context, u domain.User, token string) error {
	st, err := s.Status(ctx, u, token)
	if err != nil {
		return err
	}
	if !st.Enabled {
		return ErrTwoFactorSetup
	}
	if !st.Verified {
		return ErrTwoFactorRequired
	}
	return nil
}

// Setup — новый секрет (пока 2FA не подтверждена): ключ, ссылка и QR-код для приложения.
func (s *TwoFactor) Setup(ctx context.Context, u domain.User) (TwoFactorSetup, error) {
	if !u.Staff() || u.Blocked() {
		return TwoFactorSetup{}, ErrNotStaff
	}
	secret := make([]byte, 20)
	if _, err := io.ReadFull(s.d.Random, secret); err != nil {
		return TwoFactorSetup{}, err
	}
	sealed, err := s.d.Cipher.Seal(secret, totpAAD(u.ID))
	if err != nil {
		return TwoFactorSetup{}, err
	}
	if err := s.d.Repo.SaveTOTPSecret(ctx, u.ID, sealed); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return TwoFactorSetup{}, ErrWrongStatus
		}
		return TwoFactorSetup{}, err
	}
	account := u.Email
	if account == "" {
		account = "id" + strconv.FormatInt(u.ID, 10)
	}
	out := TwoFactorSetup{Secret: b32.EncodeToString(secret), URI: totpURI(s.cfg.Issuer, account, secret)}
	if s.d.QR != nil {
		if out.QR, err = s.d.QR.PNG(out.URI); err != nil {
			s.d.Log.WarnContext(ctx, "2FA: QR-код не построен", "err", err)
		}
	}
	return out, nil
}

// Confirm — первый код из приложения: 2FA включается, сессия подтверждается, выдаются резервные коды (показываются
// один раз).
func (s *TwoFactor) Confirm(ctx context.Context, u domain.User, token, code string) ([]string, error) {
	if !u.Staff() || u.Blocked() {
		return nil, ErrNotStaff
	}
	t, err := s.d.Repo.TOTP(ctx, u.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, ErrWrongStatus
	}
	if err != nil {
		return nil, err
	}
	if t.ConfirmedAt != nil {
		return nil, ErrWrongStatus
	}
	if err := s.checkTOTP(ctx, u, t, normalizeOTP(code), true); err != nil {
		return nil, err
	}
	codes, err := s.issueBackup(ctx, u, domain.AuditTwoFAEnabled)
	if err != nil {
		return nil, err
	}
	return codes, s.markSession(ctx, token)
}

// Verify — подтверждение 2FA в сессии: код из приложения или резервный код (гасится).
func (s *TwoFactor) Verify(ctx context.Context, u domain.User, token, code string) error {
	if !u.Staff() || u.Blocked() {
		return ErrNotStaff
	}
	t, err := s.d.Repo.TOTP(ctx, u.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return ErrTwoFactorSetup
	}
	if err != nil {
		return err
	}
	if t.ConfirmedAt == nil {
		return ErrTwoFactorSetup
	}
	c := normalizeOTP(code)
	if isDigits(c, totpDigits) {
		err = s.checkTOTP(ctx, u, t, c, false)
	} else {
		err = s.checkBackup(ctx, u, t, c)
	}
	if err != nil {
		return err
	}
	return s.markSession(ctx, token)
}

// RegenerateBackup — новые резервные коды (старые перестают действовать).
func (s *TwoFactor) RegenerateBackup(ctx context.Context, u domain.User) ([]string, error) {
	if !u.Staff() || u.Blocked() {
		return nil, ErrNotStaff
	}
	return s.issueBackup(ctx, u, domain.AuditBackupRegen)
}

// Reset — администратор сбрасывает 2FA сотрудника (потерян телефон): при следующем входе — настройка заново.
func (s *TwoFactor) Reset(ctx context.Context, actor domain.User, targetID int64) error {
	if err := authorize(actor, domain.PermManageStaff); err != nil {
		return err
	}
	if targetID == actor.ID {
		return ErrSelfAction
	}
	e := auditFor(ctx, actor, domain.AuditTwoFAReset, "user", strconv.FormatInt(targetID, 10))
	return s.d.Repo.ResetTwoFactor(ctx, targetID, e)
}

func (s *TwoFactor) locked(t domain.TOTP) error {
	if t.LockedUntil != nil && t.LockedUntil.After(s.d.Clock.Now()) {
		return &LimitError{Reason: "2fa_locked", RetryAfter: t.LockedUntil.Sub(s.d.Clock.Now())}
	}
	return nil
}

// checkTOTP — код подходит (окно ±30 с), шаг ещё не использован; иначе — счётчик неверных и блокировка.
func (s *TwoFactor) checkTOTP(ctx context.Context, u domain.User, t domain.TOTP, code string, confirming bool) error {
	if err := s.locked(t); err != nil {
		return err
	}
	secret, err := s.d.Cipher.Open(t.Secret, totpAAD(u.ID))
	if err != nil {
		return err
	}
	now := s.d.Clock.Now()
	if step, ok := totpMatch(secret, code, now); ok && isDigits(code, totpDigits) {
		accepted, err := s.d.Repo.AcceptTOTPStep(ctx, u.ID, step, confirming, now)
		if err != nil {
			return err
		}
		if accepted {
			return nil
		}
	}
	return s.failure(ctx, u)
}

// checkBackup — резервный код: 10 знаков, гасится атомарно.
func (s *TwoFactor) checkBackup(ctx context.Context, u domain.User, t domain.TOTP, code string) error {
	if err := s.locked(t); err != nil {
		return err
	}
	if len(code) == backupLen {
		ok, err := s.d.Repo.UseBackupCode(ctx, u.ID, s.backupHash(u.ID, code), s.d.Clock.Now())
		if err != nil {
			return err
		}
		if ok {
			e := auditFor(ctx, u, domain.AuditBackupUsed, "user", strconv.FormatInt(u.ID, 10))
			if err := s.d.Repo.Audit(ctx, e); err != nil {
				s.d.Log.WarnContext(ctx, "2FA: журнал", "err", err)
			}
			return nil
		}
	}
	return s.failure(ctx, u)
}

func (s *TwoFactor) failure(ctx context.Context, u domain.User) error {
	locked, err := s.d.Repo.TOTPFailure(ctx, u.ID, s.cfg.MaxFailures, s.d.Clock.Now().Add(s.cfg.LockFor))
	if err != nil {
		return err
	}
	if locked {
		e := auditFor(ctx, u, domain.AuditTwoFAFailed, "user", strconv.FormatInt(u.ID, 10))
		if err := s.d.Repo.Audit(ctx, e); err != nil {
			s.d.Log.WarnContext(ctx, "2FA: журнал", "err", err)
		}
		s.d.Log.WarnContext(ctx, "2FA: перебор кодов — блокировка", "user", u.ID)
		return &LimitError{Reason: "2fa_locked", RetryAfter: s.cfg.LockFor}
	}
	return ErrTwoFactorInvalid
}

func (s *TwoFactor) markSession(ctx context.Context, token string) error {
	now := s.d.Clock.Now()
	err := s.d.Sessions.SetSessionMFA(ctx, hashToken(token), now, now)
	if errors.Is(err, domain.ErrNotFound) {
		return ErrUnauthorized
	}
	return err
}

// Резервный код: 10 знаков base32 (50 бит), показывается как «abcde-fghij».
const backupLen = 10

func (s *TwoFactor) backupHash(userID int64, code string) []byte {
	m := hmac.New(sha256.New, s.cfg.Secret)
	m.Write([]byte("backup:" + strconv.FormatInt(userID, 10) + ":"))
	m.Write([]byte(code))
	return m.Sum(nil)
}

func (s *TwoFactor) issueBackup(ctx context.Context, u domain.User, action string) ([]string, error) {
	codes := make([]string, 0, s.cfg.BackupCodes)
	hashes := make([][]byte, 0, s.cfg.BackupCodes)
	for len(codes) < s.cfg.BackupCodes {
		b := make([]byte, 7)
		if _, err := io.ReadFull(s.d.Random, b); err != nil {
			return nil, err
		}
		raw := normalizeOTP(b32.EncodeToString(b))[:backupLen]
		codes = append(codes, raw[:5]+"-"+raw[5:])
		hashes = append(hashes, s.backupHash(u.ID, raw))
	}
	e := auditFor(ctx, u, action, "user", strconv.FormatInt(u.ID, 10))
	e.After = map[string]int{"backupCodes": len(codes)}
	if err := s.d.Repo.ReplaceBackupCodes(ctx, u.ID, hashes, e); err != nil {
		return nil, err
	}
	return codes, nil
}

// TOTPKeyFromSecret — ключ шифрования секретов 2FA из AUTH_SECRET (когда TOKEN_ENC_KEY не задан): HMAC-SHA-256 с
// назначением, в base64 — формат ключа secretbox.
func TOTPKeyFromSecret(secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("ohvat:2fa-secret-key:v1"))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}
