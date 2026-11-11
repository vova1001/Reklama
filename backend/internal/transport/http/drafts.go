package httpapi

import (
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// Черновики форм редактора: состояние формы как есть (JSON-объект) — сохраняется при каждой правке.

type draftOut struct {
	Draft *draftDTO `json:"draft"`
}

type draftDTO struct {
	Form    string    `json:"form"`
	Data    rawJSON   `json:"data"`
	SavedAt time.Time `json:"savedAt"`
}

type draftSaved struct {
	SavedAt time.Time `json:"savedAt"`
}

// rawJSON — JSON как есть (без повторного разбора).
type rawJSON []byte

func (r rawJSON) MarshalJSON() ([]byte, error) { return r, nil }

// GET /api/me/drafts/{form} — последнее состояние формы (draft: null — нет).
func (s *Server) getDraft(w http.ResponseWriter, r *http.Request) {
	d, err := s.drafts.Get(r.Context(), currentUser(r), chi.URLParam(r, "form"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if d == nil {
		writeJSON(w, http.StatusOK, draftOut{})
		return
	}
	writeJSON(w, http.StatusOK, draftOut{Draft: &draftDTO{Form: d.Form, Data: d.Data, SavedAt: d.SavedAt.UTC()}})
}

// PUT /api/me/drafts/{form} — сохранить состояние формы (тело — JSON-объект, до DRAFT_MAX_KB).
func (s *Server) putDraft(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(s.drafts.MaxBytes())+1))
	if err != nil || len(body) > s.drafts.MaxBytes() {
		writeError(w, http.StatusRequestEntityTooLarge, "draft_too_large", "Черновик слишком большой — сократите текст.")
		return
	}
	at, err := s.drafts.Save(r.Context(), currentUser(r), chi.URLParam(r, "form"), body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, draftSaved{SavedAt: at})
}

// DELETE /api/me/drafts/{form}
func (s *Server) deleteDraft(w http.ResponseWriter, r *http.Request) {
	if err := s.drafts.Delete(r.Context(), currentUser(r), chi.URLParam(r, "form")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
