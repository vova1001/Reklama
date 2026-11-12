package httpapi

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// POST /api/me/media — загрузка фото: multipart (purpose, crop «x,y,w,h» в долях — необязательно, file).
// Тело режется по пределу сервиса; тип проверяет сервис по содержимому.
func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	maxFile := s.media.MaxBytes()
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
	in := service.UploadInput{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.uploadReadFail(w, r, err)
			return
		}
		switch part.FormName() {
		case "purpose":
			b, _ := io.ReadAll(io.LimitReader(part, 64))
			in.Purpose = strings.TrimSpace(string(b))
		case "crop":
			b, _ := io.ReadAll(io.LimitReader(part, 128))
			c, ok := parseCrop(string(b))
			if !ok {
				writeJSON(w, http.StatusBadRequest, errorDTO{Error: errorBody{Code: "validation", Message: "Проверьте поля формы.", Fields: map[string]string{"crop": "Неверная область кадра"}}})
				return
			}
			in.Crop = c
		case "file":
			data, err := io.ReadAll(io.LimitReader(part, maxFile+1))
			if err != nil {
				s.uploadReadFail(w, r, err)
				return
			}
			if int64(len(data)) > maxFile {
				s.fail(w, r, service.ErrImageTooLarge)
				return
			}
			in.Data = data
		}
		_ = part.Close()
	}
	if in.Data == nil {
		writeJSON(w, http.StatusBadRequest, errorDTO{Error: errorBody{Code: "validation", Message: "Проверьте поля формы.", Fields: map[string]string{"file": "Выберите фото"}}})
		return
	}
	m, err := s.media.Upload(r.Context(), currentUser(r), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMediaDTO(m))
}

func (s *Server) uploadReadFail(w http.ResponseWriter, r *http.Request, err error) {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		s.fail(w, r, service.ErrImageTooLarge)
		return
	}
	slog.InfoContext(r.Context(), "загрузка фото прервана", "err", err)
	writeError(w, http.StatusBadRequest, "bad_request", "Загрузка прервалась. Попробуйте ещё раз.")
}

// parseCrop — «x,y,w,h» (доли 0–1); пусто — без обрезки.
func parseCrop(s string) (*service.CropRect, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	p := strings.Split(s, ",")
	if len(p) != 4 {
		return nil, false
	}
	var v [4]float64
	for i, x := range p {
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return nil, false
		}
		v[i] = f
	}
	return &service.CropRect{X: v[0], Y: v[1], W: v[2], H: v[3]}, true
}

// DELETE /api/me/media/{id} — удалить своё неприкреплённое фото.
func (s *Server) deleteMedia(w http.ResponseWriter, r *http.Request) {
	if err := s.media.Delete(r.Context(), currentUser(r), chi.URLParam(r, "id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/me/avatar — аватар профиля из загруженного фото (mediaId: null — убрать).
func (s *Server) putAvatar(w http.ResponseWriter, r *http.Request) {
	var in avatarIn
	if !decode(w, r, &in) {
		return
	}
	id := ""
	if in.MediaID != nil {
		id = *in.MediaID
	}
	url, err := s.media.SetAvatar(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, avatarOut{AvatarURL: url})
}

// GET /media/{id}/{size}.webp — файл фото. Опубликованное и аватары — всем (долгий кэш), остальное — только
// владельцу; чужое закрытое — 404, как несуществующее. Содержимое по адресу не меняется (ETag — id и размер).
func (s *Server) getMedia(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	name := chi.URLParam(r, "file")
	size, err := strconv.Atoi(strings.TrimSuffix(name, ".webp"))
	video := name == "video" // видео страницы — один файл как есть (size 0)
	if video {
		size, err = 0, nil
	} else if err != nil || !strings.HasSuffix(name, ".webp") || size == 0 {
		http.NotFound(w, r)
		return
	}
	var viewer domain.User
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if u, err := s.auth.Session(r.Context(), c.Value); err == nil {
			viewer = u
		}
	}
	b, public, err := s.media.Open(r.Context(), viewer.ID, id, size)
	// фото на проверке видит сотрудник с подтверждённой 2FA (карточка проверки)
	if isNotFound(err) && viewer.Staff() && s.twoFactor != nil && s.twoFactor.Require(r.Context(), viewer, sessionToken(r)) == nil {
		b, err = s.media.OpenForReview(r.Context(), viewer, id, size)
		public = false
	}
	if err != nil {
		var code int
		switch {
		case isNotFound(err):
			code = http.StatusNotFound
		default:
			slog.ErrorContext(r.Context(), "фото: чтение", "err", err)
			code = http.StatusInternalServerError
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, http.StatusText(code), code)
		return
	}
	etag := `"` + id + "-" + strconv.Itoa(size) + `"`
	h := w.Header()
	h.Set("ETag", etag)
	if video {
		serveVideo(w, r, b, public)
		return
	}
	h.Set("Content-Type", "image/webp")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if public {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "private, max-age=3600")
		h.Set("Vary", "Cookie")
	}
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(b.Data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b.Data)
	}
}

// serveVideo — видео с поддержкой Range (перемотка, докачка); отдача может идти дольше обычного ответа.
func serveVideo(w http.ResponseWriter, r *http.Request, b service.Blob, public bool) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(videoUploadTimeout))
	h := w.Header()
	ct := b.ContentType
	if ct != "video/mp4" && ct != "video/webm" {
		ct = "application/octet-stream"
	}
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if public {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "private, max-age=3600")
		h.Set("Vary", "Cookie")
	}
	http.ServeContent(w, r, "", b.ModTime, bytes.NewReader(b.Data))
}
