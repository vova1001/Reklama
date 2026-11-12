package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"time"

	"ohvat/backend/internal/domain"
)

// DraftStore — где лежат состояния форм: Redis (основное) или PostgreSQL (запасное). Нет — domain.ErrNotFound.
type DraftStore interface {
	GetDraft(ctx context.Context, userID int64, form string) (domain.FormDraft, error)
	// PutDraft — сохранить (заменить); живёт ttl с момента сохранения.
	PutDraft(ctx context.Context, userID int64, d domain.FormDraft, ttl time.Duration) error
	DeleteDraft(ctx context.Context, userID int64, form string) error
}

// ErrDraftTooLarge — состояние формы больше предела.
var ErrDraftTooLarge = errors.New("черновик слишком большой")

// DraftConfig — пределы черновиков форм.
type DraftConfig struct {
	MaxBytes int           // размер JSON, 64 КБ
	TTL      time.Duration // срок жизни с последнего сохранения, 7 дней
}

// Drafts — черновики форм редактора: сохранить при каждой правке, восстановить при открытии, удалить после отправки.
type Drafts struct {
	store DraftStore
	cfg   DraftConfig
	clock Clock
}

// NewDrafts создаёт сервис черновиков форм.
func NewDrafts(store DraftStore, cfg DraftConfig, clock Clock) *Drafts {
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 64 << 10
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 7 * 24 * time.Hour
	}
	if clock == nil {
		clock = SystemClock{}
	}
	return &Drafts{store: store, cfg: cfg, clock: clock}
}

// reForm — какие формы бывают: новое задание, задание по id, анкета, страница бизнеса и профиль автора.
var reForm = regexp.MustCompile(`^(task-new|task-[1-9][0-9]{0,17}|card|page-business|page-creator)$`)

func checkForm(form string) error {
	if !reForm.MatchString(form) {
		return domain.ErrNotFound
	}
	return nil
}

// Get — последнее состояние формы; нет — nil.
func (s *Drafts) Get(ctx context.Context, u domain.User, form string) (*domain.FormDraft, error) {
	if err := checkForm(form); err != nil {
		return nil, err
	}
	d, err := s.store.GetDraft(ctx, u.ID, form)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Save — сохранить состояние формы (JSON-объект не больше предела). Возвращает время сохранения.
func (s *Drafts) Save(ctx context.Context, u domain.User, form string, data []byte) (time.Time, error) {
	if err := checkForm(form); err != nil {
		return time.Time{}, err
	}
	if len(data) > s.cfg.MaxBytes {
		return time.Time{}, ErrDraftTooLarge
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return time.Time{}, &ValidationError{Fields: map[string]string{"data": "Черновик — объект JSON"}}
	}
	at := s.clock.Now().UTC()
	return at, s.store.PutDraft(ctx, u.ID, domain.FormDraft{Form: form, Data: data, SavedAt: at}, s.cfg.TTL)
}

// Delete — убрать черновик формы (после отправки или удаления).
func (s *Drafts) Delete(ctx context.Context, u domain.User, form string) error {
	if err := checkForm(form); err != nil {
		return err
	}
	return s.store.DeleteDraft(ctx, u.ID, form)
}

// FallbackDrafts — основное хранилище (Redis) и запасное (PostgreSQL): основное недоступно — пишем и читаем запасное;
// в основном нет — смотрим в запасном (туда могли сохранить, пока Redis был недоступен).
type FallbackDrafts struct {
	Primary, Secondary DraftStore
	Log                *slog.Logger
}

var _ DraftStore = FallbackDrafts{}

// GetDraft — из основного, иначе из запасного.
func (f FallbackDrafts) GetDraft(ctx context.Context, userID int64, form string) (domain.FormDraft, error) {
	d, err := f.Primary.GetDraft(ctx, userID, form)
	if err == nil {
		return d, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		f.warn("чтение", err)
	}
	return f.Secondary.GetDraft(ctx, userID, form)
}

// PutDraft — в основное; не вышло — в запасное. Запасная копия читается, только когда в основном нет, — поэтому
// после возврата Redis она не мешает (и уходит при удалении черновика или по сроку).
func (f FallbackDrafts) PutDraft(ctx context.Context, userID int64, d domain.FormDraft, ttl time.Duration) error {
	if err := f.Primary.PutDraft(ctx, userID, d, ttl); err != nil {
		f.warn("запись", err)
		return f.Secondary.PutDraft(ctx, userID, d, ttl)
	}
	return nil
}

// DeleteDraft — из обоих.
func (f FallbackDrafts) DeleteDraft(ctx context.Context, userID int64, form string) error {
	err1 := f.Primary.DeleteDraft(ctx, userID, form)
	if err1 != nil {
		f.warn("удаление", err1)
	}
	return f.Secondary.DeleteDraft(ctx, userID, form)
}

func (f FallbackDrafts) warn(op string, err error) {
	if f.Log != nil {
		f.Log.Warn("черновики: Redis недоступен — PostgreSQL", "op", op, "err", err)
	}
}

// DraftCleaner — удаление истёкших запасных копий (у Redis срок свой).
type DraftCleaner interface {
	Cleanup(ctx context.Context, before time.Time) (int64, error)
}

// RunCleanup — раз в every удалять запасные копии старше срока, пока не отменён ctx.
func (s *Drafts) RunCleanup(ctx context.Context, c DraftCleaner, every time.Duration, log *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if n, err := c.Cleanup(ctx, s.clock.Now().Add(-s.cfg.TTL)); err != nil {
			log.Error("черновики форм: очистка", "err", err)
		} else if n > 0 {
			log.Info("черновики форм: удалены старые", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// MaxBytes — предел размера состояния формы.
func (s *Drafts) MaxBytes() int { return s.cfg.MaxBytes }
