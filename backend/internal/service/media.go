package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ohvat/backend/internal/domain"
)

// ===== зависимости (только интерфейсы) =====

// MediaRepository — метаданные фото.
type MediaRepository interface {
	CreateMedia(ctx context.Context, m domain.Media) (domain.Media, error)
	OwnMedia(ctx context.Context, ownerID int64, ids []string) ([]domain.Media, error)
	Usage(ctx context.Context, ownerID int64) (files int, bytes int64, err error)
	// Access — владелец и видно ли всем; нет — domain.ErrNotFound.
	Access(ctx context.Context, id string) (domain.MediaAccess, error)
	// DeleteLoose — удалить своё неприкреплённое; нет или прикреплено — domain.ErrNotFound.
	DeleteLoose(ctx context.Context, ownerID int64, id string) error
	// SetAvatar — аватар профиля (id "" — убрать); чужое или не аватар — domain.ErrConflict.
	SetAvatar(ctx context.Context, userID int64, id, url string) error
	Orphans(ctx context.Context, before time.Time, limit int) ([]string, error)
	DeleteMedia(ctx context.Context, ids []string) error
}

// MediaLookup — свои фото по id (для прикрепления к заданию и анкете).
type MediaLookup interface {
	OwnMedia(ctx context.Context, ownerID int64, ids []string) ([]domain.Media, error)
}

// Blob — файл из хранилища.
type Blob struct {
	ContentType string
	Data        []byte
	ModTime     time.Time
}

// BlobStore — хранилище файлов: PostgreSQL или S3-совместимое (gateway/storage), выбирается настройками.
type BlobStore interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	// Get — файл; нет — domain.ErrNotFound.
	Get(ctx context.Context, key string) (Blob, error)
	Delete(ctx context.Context, keys []string) error
}

// CropRect — область кадра в долях исходного (0–1), после поворота по EXIF.
type CropRect struct{ X, Y, W, H float64 }

// ImageOptions — как обработать фото.
type ImageOptions struct {
	Aspect float64   // ширина/высота результата; 0 — как есть
	Crop   *CropRect // nil — по центру (если задан Aspect)
	Sizes  []int     // длинная сторона каждого варианта; больше исходного не растягиваем
}

// ProcessedImage — варианты в WebP (без EXIF и геометок) по размеру.
type ProcessedImage struct {
	Width, Height int // самый большой вариант
	Variants      map[int][]byte
}

// ImageProcessor — проверка и перекодирование фото (gateway/imaging: пул воркеров поверх декодеров).
type ImageProcessor interface {
	Process(ctx context.Context, data []byte, opt ImageOptions) (ProcessedImage, error)
}

// ===== ошибки =====

var (
	ErrImageType     = errors.New("не фото или формат не поддерживается") // не JPEG/PNG/WebP/HEIC, анимация, SVG
	ErrImageTooLarge = errors.New("слишком большое фото")                 // по байтам или пикселям
	ErrMediaQuota    = errors.New("место для фото закончилось")
	ErrBusy          = errors.New("обработка фото перегружена")
	ErrVideoType     = errors.New("не видео или формат не поддерживается") // не MP4 / WebM
	ErrVideoTooLarge = errors.New("слишком большое видео")
	// ErrUploadInterrupted — файл не дочитан (связь оборвалась); в цепочке — исходная ошибка чтения.
	ErrUploadInterrupted = errors.New("загрузка прервалась")
)

// ===== сервис =====

// MediaConfig — лимиты фото.
type MediaConfig struct {
	MaxBytes   int64         // один файл, 10 МБ
	MaxFiles   int           // фото на пользователя, 300
	QuotaBytes int64         // место на пользователя (все размеры), 300 МБ
	OrphanTTL  time.Duration // неприкреплённые живут, 24 ч
	// Видео (страницы): файл как есть, без перекодирования.
	VideoMaxBytes int64 // один файл, 50 МБ
	VideoSlots    int   // одновременных загрузок видео (файл целиком в памяти), 2
}

func (c *MediaConfig) defaults() {
	if c.MaxBytes <= 0 {
		c.MaxBytes = 10 << 20
	}
	if c.MaxFiles <= 0 {
		c.MaxFiles = 300
	}
	if c.QuotaBytes <= 0 {
		c.QuotaBytes = 300 << 20
	}
	if c.OrphanTTL <= 0 {
		c.OrphanTTL = 24 * time.Hour
	}
	if c.VideoMaxBytes <= 0 {
		c.VideoMaxBytes = 50 << 20
	}
	if c.VideoSlots <= 0 {
		c.VideoSlots = 2
	}
}

// Media — загрузка, просмотр с правами, аватар, очистка неприкреплённых.
type Media struct {
	repo  MediaRepository
	blobs BlobStore
	img   ImageProcessor
	clock Clock
	rnd   io.Reader
	cfg   MediaConfig
	log   *slog.Logger
	video chan struct{} // слоты загрузки видео
}

// NewMedia создаёт сервис фото.
func NewMedia(repo MediaRepository, blobs BlobStore, img ImageProcessor, cfg MediaConfig, log *slog.Logger) *Media {
	cfg.defaults()
	if log == nil {
		log = slog.Default()
	}
	return &Media{repo: repo, blobs: blobs, img: img, clock: SystemClock{}, rnd: rand.Reader, cfg: cfg, log: log,
		video: make(chan struct{}, cfg.VideoSlots)}
}

// MaxBytes — предел размера файла (транспорт режет тело запроса по нему).
func (s *Media) MaxBytes() int64 { return s.cfg.MaxBytes }

// UploadInput — загружаемое фото.
type UploadInput struct {
	Purpose string
	Data    []byte
	Crop    *CropRect
}

// Upload — проверить тип по содержимому, квоту, перекодировать в WebP (3 размера, без EXIF), сохранить.
func (s *Media) Upload(ctx context.Context, u domain.User, in UploadInput) (domain.Media, error) {
	if !domain.ValidPurpose(in.Purpose) {
		return domain.Media{}, &ValidationError{Fields: map[string]string{"purpose": "Неизвестное назначение фото"}}
	}
	if len(in.Data) == 0 {
		return domain.Media{}, &ValidationError{Fields: map[string]string{"file": "Файл пустой"}}
	}
	if int64(len(in.Data)) > s.cfg.MaxBytes {
		return domain.Media{}, ErrImageTooLarge
	}
	if in.Crop != nil && !validCrop(*in.Crop) {
		return domain.Media{}, &ValidationError{Fields: map[string]string{"crop": "Неверная область кадра"}}
	}
	files, used, err := s.repo.Usage(ctx, u.ID)
	if err != nil {
		return domain.Media{}, err
	}
	if files >= s.cfg.MaxFiles || used >= s.cfg.QuotaBytes {
		return domain.Media{}, ErrMediaQuota
	}
	p, err := s.img.Process(ctx, in.Data, ImageOptions{Aspect: domain.Aspect(in.Purpose), Crop: in.Crop, Sizes: domain.MediaSizes})
	if err != nil {
		return domain.Media{}, err
	}
	id, err := s.newID()
	if err != nil {
		return domain.Media{}, err
	}
	keys := make([]string, 0, len(p.Variants))
	var total int64
	for size, data := range p.Variants {
		k := domain.MediaKey(id, size)
		if err := s.blobs.Put(ctx, k, "image/webp", data); err != nil {
			s.dropBlobs(keys)
			return domain.Media{}, fmt.Errorf("фото в хранилище: %w", err)
		}
		keys = append(keys, k)
		total += int64(len(data))
	}
	m, err := s.repo.CreateMedia(ctx, domain.Media{ID: id, OwnerID: u.ID, Purpose: in.Purpose, Width: p.Width, Height: p.Height, Bytes: total})
	if err != nil {
		s.dropBlobs(keys)
		return domain.Media{}, err
	}
	return m, nil
}

// Delete — удалить своё неприкреплённое фото (прикреплённое убирается сохранением задания или анкеты).
func (s *Media) Delete(ctx context.Context, u domain.User, id string) error {
	if !validMediaID(id) {
		return domain.ErrNotFound
	}
	if err := s.repo.DeleteLoose(ctx, u.ID, id); err != nil {
		return err
	}
	s.dropBlobs(mediaKeys(id))
	return nil
}

// SetAvatar — аватар профиля из своего фото назначения «avatar»; id "" — убрать. Возвращает адрес аватара.
func (s *Media) SetAvatar(ctx context.Context, u domain.User, id string) (string, error) {
	url := ""
	if id != "" {
		if !validMediaID(id) {
			return "", &ValidationError{Fields: map[string]string{"mediaId": "Загрузите фото ещё раз"}}
		}
		url = MediaURL(id, 320)
	}
	err := s.repo.SetAvatar(ctx, u.ID, id, url)
	if errors.Is(err, domain.ErrConflict) {
		return "", &ValidationError{Fields: map[string]string{"mediaId": "Загрузите фото ещё раз"}}
	}
	return url, err
}

// Open — файл фото размера size (0 — видео) для зрителя (0 — гость). Не опубликованное видит только владелец; чужое закрытое
// неотличимо от несуществующего (domain.ErrNotFound). public — можно кэшировать всем.
func (s *Media) Open(ctx context.Context, viewerID int64, id string, size int) (Blob, bool, error) {
	if !validMediaID(id) || (size != 0 && !domain.ValidMediaSize(size)) {
		return Blob{}, false, domain.ErrNotFound
	}
	a, err := s.repo.Access(ctx, id)
	if err != nil {
		return Blob{}, false, err
	}
	if !a.Public && (viewerID == 0 || viewerID != a.OwnerID) {
		return Blob{}, false, domain.ErrNotFound
	}
	b, err := s.blobs.Get(ctx, fileKey(id, size))
	return b, a.Public, err
}

// fileKey — ключ фото размера size или видео (size 0).
func fileKey(id string, size int) string {
	if size == 0 {
		return domain.VideoKey(id)
	}
	return domain.MediaKey(id, size)
}

// Cleanup — удалить неприкреплённые дольше OrphanTTL (сначала файлы, потом записи). Возвращает сколько удалено.
func (s *Media) Cleanup(ctx context.Context) (int, error) {
	ids, err := s.repo.Orphans(ctx, s.clock.Now().Add(-s.cfg.OrphanTTL), 200)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	keys := make([]string, 0, len(ids)*len(domain.MediaSizes))
	for _, id := range ids {
		keys = append(keys, mediaKeys(id)...)
	}
	if err := s.repo.DeleteMedia(ctx, ids); err != nil {
		return 0, err
	}
	if err := s.blobs.Delete(ctx, keys); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// RunCleanup — очистка раз в every, пока жив ctx.
func (s *Media) RunCleanup(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if n, err := s.Cleanup(ctx); err != nil {
			s.log.Error("фото: очистка", "err", err)
		} else if n > 0 {
			s.log.Info("фото: удалены неприкреплённые", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// MediaURL — адрес фото размера size.
func MediaURL(id string, size int) string { return "/media/" + id + "/" + strconv.Itoa(size) + ".webp" }

func (s *Media) newID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(s.rnd, b); err != nil {
		return "", fmt.Errorf("id фото: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// dropBlobs — удалить файлы без контекста запроса (запрос мог быть отменён); ошибка — в журнал, сироты в
// хранилище без записи не видны и не мешают.
func (s *Media) dropBlobs(keys []string) {
	if len(keys) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.blobs.Delete(ctx, keys); err != nil {
		s.log.Warn("фото: файлы не удалены", "err", err, "keys", len(keys))
	}
}

// mediaKeys — все возможные файлы записи: размеры фото и видео (лишние ключи хранилище пропускает).
func mediaKeys(id string) []string {
	out := make([]string, 0, len(domain.MediaSizes)+1)
	for _, sz := range domain.MediaSizes {
		out = append(out, domain.MediaKey(id, sz))
	}
	return append(out, domain.VideoKey(id))
}

var reMediaID = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)

func validMediaID(id string) bool { return reMediaID.MatchString(id) }

func validCrop(c CropRect) bool {
	const eps = 1e-6
	return c.X >= 0 && c.Y >= 0 && c.W > 0.01 && c.H > 0.01 && c.X+c.W <= 1+eps && c.Y+c.H <= 1+eps
}

// mediaSpec — какие фото ждём в поле: назначение, сколько, к чему прикрепляются.
type mediaSpec struct {
	field   string
	purpose string
	max     int
	subject domain.Subject
	id      int64 // 0 — новое (фото должны быть не прикреплены)
}

// checkMedia — фото для задания или анкеты: свои, нужного назначения, не прикреплены к другому, не больше max.
func checkMedia(ctx context.Context, look MediaLookup, ve *ValidationError, ownerID int64, ids []string, sp mediaSpec) ([]string, error) {
	field, purpose, max := sp.field, sp.purpose, sp.max
	ids = uniq(ids)
	if len(ids) > max {
		ve.add(field, fmt.Sprintf("Не больше %d фото", max))
		return ids, nil
	}
	for _, id := range ids {
		if !validMediaID(id) {
			ve.add(field, "Загрузите фото ещё раз")
			return ids, nil
		}
	}
	if len(ids) == 0 {
		return ids, nil
	}
	ms, err := look.OwnMedia(ctx, ownerID, ids)
	if err != nil {
		return nil, err
	}
	ok := 0
	for _, m := range ms {
		free := m.Subject == "" || (m.Subject == string(sp.subject) && m.SubjectID == sp.id && sp.id != 0)
		if m.Purpose == purpose && free {
			ok++
		}
	}
	if ok != len(ids) {
		ve.add(field, "Загрузите фото ещё раз")
	}
	return ids, nil
}

// OpenForReview — любое фото для сотрудника с правом смотреть непубличное (карточка проверки, жалобы).
func (s *Media) OpenForReview(ctx context.Context, viewer domain.User, id string, size int) (Blob, error) {
	if err := authorize(viewer, domain.PermViewPrivates); err != nil {
		return Blob{}, domain.ErrNotFound
	}
	if !validMediaID(id) || (size != 0 && !domain.ValidMediaSize(size)) {
		return Blob{}, domain.ErrNotFound
	}
	if _, err := s.repo.Access(ctx, id); err != nil {
		return Blob{}, err
	}
	return s.blobs.Get(ctx, fileKey(id, size))
}

// ===== видео =====

// MaxVideoBytes — предел размера видео (транспорт режет тело запроса по нему).
func (s *Media) MaxVideoBytes() int64 { return s.cfg.VideoMaxBytes }

// UploadVideo — видео для страницы: файл как есть (без перекодирования), тип — по содержимому (MP4 или WebM),
// размер — до VideoMaxBytes, в квоте пользователя. Одновременно — не больше VideoSlots загрузок (файл целиком
// в памяти); заняты дольше 3 с — ErrBusy.
func (s *Media) UploadVideo(ctx context.Context, u domain.User, body io.Reader) (domain.Media, error) {
	wait := time.NewTimer(3 * time.Second)
	defer wait.Stop()
	select {
	case s.video <- struct{}{}:
		defer func() { <-s.video }()
	case <-wait.C:
		return domain.Media{}, ErrBusy
	case <-ctx.Done():
		return domain.Media{}, ctx.Err()
	}
	data, err := io.ReadAll(io.LimitReader(body, s.cfg.VideoMaxBytes+1))
	if err != nil {
		return domain.Media{}, errors.Join(ErrUploadInterrupted, err)
	}
	if len(data) == 0 {
		return domain.Media{}, &ValidationError{Fields: map[string]string{"file": "Файл пустой"}}
	}
	if int64(len(data)) > s.cfg.VideoMaxBytes {
		return domain.Media{}, ErrVideoTooLarge
	}
	ct := SniffVideo(data)
	if ct == "" {
		return domain.Media{}, ErrVideoType
	}
	files, used, err := s.repo.Usage(ctx, u.ID)
	if err != nil {
		return domain.Media{}, err
	}
	if files >= s.cfg.MaxFiles || used+int64(len(data)) > s.cfg.QuotaBytes {
		return domain.Media{}, ErrMediaQuota
	}
	id, err := s.newID()
	if err != nil {
		return domain.Media{}, err
	}
	key := domain.VideoKey(id)
	if err := s.blobs.Put(ctx, key, ct, data); err != nil {
		return domain.Media{}, fmt.Errorf("видео в хранилище: %w", err)
	}
	m, err := s.repo.CreateMedia(ctx, domain.Media{ID: id, OwnerID: u.ID, Purpose: domain.PurposePageVideo, Bytes: int64(len(data))})
	if err != nil {
		s.dropBlobs([]string{key})
		return domain.Media{}, err
	}
	return m, nil
}

// mp4Brands — основные бренды контейнера MP4 (ftyp), которые проигрывают браузеры. QuickTime (qt) — нет.
var mp4Brands = map[string]bool{
	"isom": true, "iso2": true, "iso3": true, "iso4": true, "iso5": true, "iso6": true, "mp41": true, "mp42": true,
	"avc1": true, "M4V ": true, "M4VP": true, "dash": true, "mmp4": true, "MSNV": true, "f4v ": true,
}

// SniffVideo — тип видео по содержимому: "video/mp4", "video/webm" или "" (не видео / не поддерживается).
func SniffVideo(b []byte) string {
	if len(b) >= 12 && string(b[4:8]) == "ftyp" && mp4Brands[string(b[8:12])] {
		return "video/mp4"
	}
	if len(b) >= 4 && b[0] == 0x1A && b[1] == 0x45 && b[2] == 0xDF && b[3] == 0xA3 {
		head := b
		if len(head) > 64 {
			head = head[:64]
		}
		if strings.Contains(string(head), "webm") {
			return "video/webm"
		}
	}
	return ""
}
