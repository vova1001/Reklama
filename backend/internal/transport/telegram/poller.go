package telegram

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// Updates — источник обновлений getUpdates (gateway/telegram.Client).
type Updates interface {
	GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]json.RawMessage, error)
}

// Poller — запасной приём обновлений длинным опросом (TELEGRAM_MODE=polling: локально, без https).
type Poller struct {
	src     Updates
	h       *Handler
	log     *slog.Logger
	timeout int
}

// NewPoller создаёт опрос.
func NewPoller(src Updates, h *Handler, log *slog.Logger) *Poller {
	if log == nil {
		log = slog.Default()
	}
	return &Poller{src: src, h: h, log: log, timeout: 25}
}

// Run опрашивает Telegram до отмены ctx. Обновление, которое не удалось обработать, запрашивается снова
// (offset не сдвигается) — не больше 3 раз, затем пропускается.
func (p *Poller) Run(ctx context.Context) {
	var offset int64
	wait := time.Second
	fails := map[int64]int{}
	for ctx.Err() == nil {
		ups, err := p.src.GetUpdates(ctx, offset, p.timeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			p.log.Warn("бот: getUpdates", "err", err, "retry_in", wait.String())
			if !sleep(ctx, wait) {
				return
			}
			if wait *= 2; wait > time.Minute {
				wait = time.Minute
			}
			continue
		}
		wait = time.Second
		for _, raw := range ups {
			var head struct {
				UpdateID int64 `json:"update_id"`
			}
			if json.Unmarshal(raw, &head) != nil {
				continue
			}
			if err := p.h.Process(ctx, raw); err != nil && fails[head.UpdateID] < 2 && ctx.Err() == nil {
				fails[head.UpdateID]++
				sleep(ctx, time.Second)
				break // повторим это обновление следующим запросом
			}
			delete(fails, head.UpdateID)
			offset = head.UpdateID + 1
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
