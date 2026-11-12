package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// PageService — страница бизнеса и профиль автора: редактор, адрес, проверка, публичный вид.
type PageService interface {
	Get(ctx context.Context, u domain.User, kind domain.PageKind, name string) (service.PageView, error)
	Save(ctx context.Context, u domain.User, kind domain.PageKind, in service.PageInput) (domain.Page, error)
	SetSlug(ctx context.Context, u domain.User, kind domain.PageKind, slug string) (domain.Page, error)
	Submit(ctx context.Context, u domain.User, kind domain.PageKind) (domain.Page, error)
	Withdraw(ctx context.Context, u domain.User, kind domain.PageKind) (domain.Page, error)
	DiscardRevision(ctx context.Context, u domain.User, kind domain.PageKind) (domain.Page, error)
	Public(ctx context.Context, kind domain.PageKind, slug string) (service.PublicPage, error)
}

// VideoService — загрузка видео для страниц (файл как есть).
type VideoService interface {
	MaxVideoBytes() int64
	UploadVideo(ctx context.Context, u domain.User, body io.Reader) (domain.Media, error)
}

// maxPageBody — тело сохранения страницы (20 блоков с текстом и ссылками).
const maxPageBody = 256 << 10

// videoUploadTimeout — сколько можно загружать видео (медленная мобильная связь; сервер по умолчанию ждёт 30 с).
const videoUploadTimeout = 10 * time.Minute

func pageKind(r *http.Request) domain.PageKind { return domain.PageKind(chi.URLParam(r, "kind")) }

// GET /api/me/pages/{kind} — своя страница (page: null — ещё нет) и адрес для новой (name — название из профиля).
func (s *Server) getMyPage(w http.ResponseWriter, r *http.Request) {
	v, err := s.pages.Get(r.Context(), currentUser(r), pageKind(r), clip(r.URL.Query().Get("name"), 200))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOut{Page: toPageDTO(v.Page), SuggestedSlug: v.SuggestedSlug})
}

// PUT /api/me/pages/{kind} — сохранить черновик (у опубликованной — правку).
func (s *Server) putMyPage(w http.ResponseWriter, r *http.Request) {
	var in pageIn
	if !decodeN(w, r, &in, maxPageBody) {
		return
	}
	p, err := s.pages.Save(r.Context(), currentUser(r), pageKind(r), in.toService())
	s.writePage(w, r, p, err)
}

// PUT /api/me/pages/{kind}/slug — сменить адрес.
func (s *Server) putPageSlug(w http.ResponseWriter, r *http.Request) {
	var in slugIn
	if !decode(w, r, &in) {
		return
	}
	p, err := s.pages.SetSlug(r.Context(), currentUser(r), pageKind(r), in.Slug)
	s.writePage(w, r, p, err)
}

// POST /api/me/pages/{kind}/submit
func (s *Server) submitPage(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.Submit(r.Context(), currentUser(r), pageKind(r))
	s.writePage(w, r, p, err)
}

// POST /api/me/pages/{kind}/withdraw
func (s *Server) withdrawPage(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.Withdraw(r.Context(), currentUser(r), pageKind(r))
	s.writePage(w, r, p, err)
}

// DELETE /api/me/pages/{kind}/revision
func (s *Server) discardPageRevision(w http.ResponseWriter, r *http.Request) {
	p, err := s.pages.DiscardRevision(r.Context(), currentUser(r), pageKind(r))
	s.writePage(w, r, p, err)
}

func (s *Server) writePage(w http.ResponseWriter, r *http.Request, p domain.Page, err error) {
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOut{Page: toPageDTO(&p)})
}

// GET /api/pages/{kind}/{slug} — опубликованная страница для всех (без входа). Прежний адрес — movedTo.
func (s *Server) getPublicPage(w http.ResponseWriter, r *http.Request) {
	v, err := s.pages.Public(r.Context(), pageKind(r), clip(chi.URLParam(r, "slug"), 64))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	if v.MovedTo != "" {
		writeJSON(w, http.StatusOK, publicPageOut{MovedTo: v.MovedTo})
		return
	}
	out := publicPageOut{Page: toPageDTO(&v.Page), Tasks: toTasksDTO(v.Tasks), Card: toCardDTO(v.Card)}
	if v.Platforms != nil {
		ps := toPlatformsDTO(v.Platforms)
		out.Platforms = &ps
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/me/media/video — загрузка видео: multipart (file). Тип и размер проверяет сервис по содержимому;
// загрузка может идти дольше обычного запроса.
func (s *Server) uploadVideo(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(videoUploadTimeout))
	_ = rc.SetWriteDeadline(time.Now().Add(videoUploadTimeout + time.Minute))
	maxFile := s.videos.MaxVideoBytes()
	ct, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || ct != "multipart/form-data" || params["boundary"] == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Не удалось прочитать файл. Попробуйте ещё раз.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFile+64<<10)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Не удалось прочитать файл. Попробуйте ещё раз.")
		return
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.videoReadFail(w, r, errors.Join(service.ErrUploadInterrupted, err))
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		m, err := s.videos.UploadVideo(r.Context(), currentUser(r), part)
		if err != nil {
			var mb *http.MaxBytesError
			if errors.As(err, &mb) {
				err = service.ErrVideoTooLarge
			}
			s.videoReadFail(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, videoDTO{ID: m.ID, Src: videoURL(m.ID), Bytes: m.Bytes})
		return
	}
	writeJSON(w, http.StatusBadRequest, errorDTO{Error: errorBody{Code: "validation", Message: "Проверьте поля формы.", Fields: map[string]string{"file": "Выберите видео"}}})
}

func (s *Server) videoReadFail(w http.ResponseWriter, r *http.Request, err error) {
	var mb *http.MaxBytesError
	switch {
	case errors.As(err, &mb), errors.Is(err, service.ErrVideoTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "video_too_large", "Видео слишком большое: до "+mbText(s.videos.MaxVideoBytes())+".")
	case errors.Is(err, service.ErrVideoType):
		writeError(w, http.StatusUnsupportedMediaType, "video_type", "Подойдёт видео MP4 или WebM.")
	case errors.Is(err, service.ErrBusy):
		w.Header().Set("Retry-After", "30")
		writeJSON(w, http.StatusServiceUnavailable, errorDTO{Error: errorBody{Code: "busy", Message: "Много загрузок видео. Попробуйте через минуту.", RetryAfter: 30}})
	case errors.Is(err, service.ErrUploadInterrupted) || !errors.Is(err, context.Canceled) && isMultipartErr(err):
		slog.InfoContext(r.Context(), "загрузка видео прервана", "err", err)
		writeError(w, http.StatusBadRequest, "bad_request", "Загрузка прервалась. Попробуйте ещё раз.")
	default:
		s.fail(w, r, err)
	}
}

// isMultipartErr — ошибка разбора тела (не сервиса).
func isMultipartErr(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }

func mbText(n int64) string { return strconv.FormatInt(n>>20, 10) + " МБ" }
