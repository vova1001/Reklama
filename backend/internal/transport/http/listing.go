package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"ohvat/backend/internal/domain"
)

// Тела форм с блоками больше обычных: до 8 блоков × 15 пунктов.
const maxListingBody = 96 << 10

// ===== задания =====

// GET /api/me/tasks — свои задания (все статусы).
func (s *Server) listMyTasks(w http.ResponseWriter, r *http.Request) {
	ts, err := s.tasks.List(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tasksOut{Tasks: toTasksDTO(ts)})
}

// POST /api/me/tasks — новый черновик.
func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var in taskIn
	if !decodeN(w, r, &in, maxListingBody) {
		return
	}
	t, err := s.tasks.Create(r.Context(), currentUser(r), in.toService())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toTaskDTO(t))
}

// GET /api/me/tasks/{id}
func (s *Server) getMyTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, err := s.tasks.Get(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTaskDTO(t))
}

// PUT /api/me/tasks/{id} — правка черновика или отклонённого.
func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in taskIn
	if !decodeN(w, r, &in, maxListingBody) {
		return
	}
	t, err := s.tasks.Update(r.Context(), currentUser(r), id, in.toService())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTaskDTO(t))
}

// DELETE /api/me/tasks/{id} — удалить черновик или отклонённое.
func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.tasks.Delete(r.Context(), currentUser(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/me/tasks/{id}/submit — на проверку.
func (s *Server) submitTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, err := s.tasks.Submit(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTaskDTO(t))
}

// POST /api/me/tasks/{id}/withdraw — с проверки в черновик.
func (s *Server) withdrawTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, err := s.tasks.Withdraw(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTaskDTO(t))
}

// GET /api/tasks — лента опубликованных (курсор cursor, limit 1–50).
func (s *Server) getFeed(w http.ResponseWriter, r *http.Request) {
	after, ok := decodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный адрес страницы ленты.")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	p, err := s.tasks.Feed(r.Context(), after, limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	writeJSON(w, http.StatusOK, feedOut{Tasks: toTasksDTO(p.Tasks), Next: encodeCursor(p.Next)})
}

// ===== анкета автора =====

// GET /api/me/card — своя анкета (card: null — ещё нет).
func (s *Server) getCard(w http.ResponseWriter, r *http.Request) {
	c, err := s.cards.Get(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cardOut{Card: toCardDTO(c)})
}

// PUT /api/me/card — сохранить черновик.
func (s *Server) putCard(w http.ResponseWriter, r *http.Request) {
	var in cardIn
	if !decodeN(w, r, &in, maxListingBody) {
		return
	}
	c, err := s.cards.Save(r.Context(), currentUser(r), in.toService())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cardOut{Card: toCardDTO(&c)})
}

// POST /api/me/card/submit
func (s *Server) submitCard(w http.ResponseWriter, r *http.Request) {
	c, err := s.cards.Submit(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cardOut{Card: toCardDTO(&c)})
}

// POST /api/me/card/withdraw
func (s *Server) withdrawCard(w http.ResponseWriter, r *http.Request) {
	c, err := s.cards.Withdraw(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cardOut{Card: toCardDTO(&c)})
}

// ===== категории =====

// GET /api/me/categories — общие и свои.
func (s *Server) getCategories(w http.ResponseWriter, r *http.Request) {
	l, err := s.categories.List(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, categoriesOut{Common: toCategoryItems(l.Common), Own: toCategoryItems(l.Own)})
}

// GET /api/me/categories/similar?q= — похожие (подсказки при вводе своей).
func (s *Server) getSimilarCategories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) > 200 {
		q = q[:200]
	}
	cs, err := s.categories.Similar(r.Context(), currentUser(r), q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, similarOut{Items: toCategoryItems(cs)})
}

// POST /api/me/categories — своя категория (или уже существующая с таким названием).
func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) {
	var in categoryIn
	if !decode(w, r, &in) {
		return
	}
	c, created, err := s.categories.Create(r.Context(), currentUser(r), in.Title)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, categoryCreated{Category: toCategoryItems([]domain.CategoryInfo{c})[0], Created: created})
}

// pathID — {id} из адреса; не число — 404.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "Не найдено.")
		return 0, false
	}
	return id, true
}
