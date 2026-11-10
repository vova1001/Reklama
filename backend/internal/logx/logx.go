// Package logx — структурный журнал: request_id из контекста попадает в каждую запись *Context.
package logx

import (
	"context"
	"log/slog"
)

type ctxKey struct{}

// WithRequestID кладёт id запроса в контекст.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// RequestID — id запроса из контекста.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// Handler добавляет request_id к записям, сделанным через slog.*Context.
type Handler struct{ slog.Handler }

// NewHandler оборачивает обработчик журнала.
func NewHandler(h slog.Handler) *Handler { return &Handler{Handler: h} }

// Handle добавляет request_id.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs сохраняет обёртку.
func (h *Handler) WithAttrs(a []slog.Attr) slog.Handler { return &Handler{h.Handler.WithAttrs(a)} }

// WithGroup сохраняет обёртку.
func (h *Handler) WithGroup(n string) slog.Handler { return &Handler{h.Handler.WithGroup(n)} }
