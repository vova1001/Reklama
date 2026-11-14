package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"ohvat/backend/internal/domain"
)

// ===== уведомления в приложении: свой ящик =====

// NoticesService — уведомления текущего пользователя.
type NoticesService interface {
	List(ctx context.Context, u domain.User, limit int) ([]domain.Notice, int64, error)
	MarkRead(ctx context.Context, u domain.User, id int64) error
	MarkAllRead(ctx context.Context, u domain.User) error
}

type noticeDTO struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	ItemID    int64      `json:"itemId"`
	Action    string     `json:"action"`
	Title     string     `json:"title"`
	Reason    string     `json:"reason"`
	Path      string     `json:"path"`
	ReadAt    *time.Time `json:"readAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

func noticeOf(n domain.Notice) noticeDTO {
	return noticeDTO{ID: n.ID, Kind: string(n.Kind), ItemID: n.ItemID, Action: string(n.Action), Title: n.Title,
		Reason: n.Reason, Path: n.Path, ReadAt: n.ReadAt, CreatedAt: n.CreatedAt}
}

// GET /api/me/notifications — последние уведомления и счётчик непрочитанных.
func (s *Server) getNotifications(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, unread, err := s.notices.List(r.Context(), u, limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]noticeDTO, 0, len(items))
	for _, n := range items {
		out = append(out, noticeOf(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "unread": unread})
}

// POST /api/me/notifications/read — прочитать одно ({"id": N}) или все (пустое тело).
func (s *Server) postNotificationsRead(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var body struct {
		ID int64 `json:"id"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) { // пустое тело — все прочитанными
		writeError(w, http.StatusBadRequest, "bad_request", "Не удалось прочитать данные формы. Обновите страницу и попробуйте снова.")
		return
	}
	var err error
	if body.ID > 0 {
		err = s.notices.MarkRead(r.Context(), u, body.ID)
	} else {
		err = s.notices.MarkAllRead(r.Context(), u)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
