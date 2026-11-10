package imaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"sync"
	"testing"
	"time"

	xwebp "golang.org/x/image/webp"

	"ohvat/backend/internal/service"
)

var sizes = []int{320, 800, 1600}

// photo — JPEG w×h: левая половина красная, правая синяя (видно поворот и обрезку).
func photo(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{220, 20, 20, 255}
			if x >= w/2 {
				c = color.RGBA{20, 20, 220, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withExif — вставить в JPEG сегмент EXIF с поворотом и «геометкой» (строка GPS — проверяем, что не уйдёт дальше).
func withExif(jpg []byte, orientation uint16) []byte {
	tiff := []byte("II*\x00\x08\x00\x00\x00")
	ifd := make([]byte, 2+12+4)
	binary.LittleEndian.PutUint16(ifd[0:], 1)
	binary.LittleEndian.PutUint16(ifd[2:], 0x0112)
	binary.LittleEndian.PutUint16(ifd[4:], 3)
	binary.LittleEndian.PutUint32(ifd[6:], 1)
	binary.LittleEndian.PutUint16(ifd[10:], orientation)
	payload := append([]byte("Exif\x00\x00"), append(tiff, ifd...)...)
	payload = append(payload, []byte("GPS 55.7558N 37.6173E")...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)
	return append(append([]byte{0xFF, 0xD8}, seg...), jpg[2:]...)
}

func decodeWebP(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := xwebp.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("не WebP: %v", err)
	}
	return img
}

func TestSniff(t *testing.T) {
	var p bytes.Buffer
	_ = png.Encode(&p, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	heicData, _ := os.ReadFile("testdata/sample.heic")
	animWebP := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8X"), make([]byte, 10)...)
	animWebP[20] = 0x02
	cases := map[string]struct {
		b    []byte
		want string
	}{
		"jpeg":     {photo(t, 4, 4), formatJPEG},
		"png":      {p.Bytes(), formatPNG},
		"apng":     {append(p.Bytes()[:33:33], []byte("\x00\x00\x00\x08acTL")...), ""},
		"heic":     {heicData, formatHEIC},
		"webp":     {append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 10)...), formatWebP},
		"anim":     {animWebP, ""},
		"gif":      {[]byte("GIF89a\x01\x00\x01\x00"), ""},
		"svg":      {[]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), ""},
		"html":     {[]byte("<html><script>alert(1)</script>"), ""},
		"empty":    {nil, ""},
		"ftyp mp4": {[]byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00isomavc1"), ""},
	}
	for name, c := range cases {
		if got := sniff(c.b); got != c.want {
			t.Errorf("%s: %q, ждали %q", name, got, c.want)
		}
	}
}

func TestProcessJPEG(t *testing.T) {
	src := withExif(photo(t, 2000, 1000), 1)
	out, err := Processor{}.Process(context.Background(), src, service.ImageOptions{Sizes: sizes})
	if err != nil {
		t.Fatal(err)
	}
	if out.Width != 1600 || out.Height != 800 || len(out.Variants) != 3 {
		t.Fatalf("%dx%d, вариантов %d", out.Width, out.Height, len(out.Variants))
	}
	for size, b := range out.Variants {
		img := decodeWebP(t, b)
		if w := img.Bounds().Dx(); w != size {
			t.Errorf("размер %d: ширина %d", size, w)
		}
		if bytes.Contains(b, []byte("GPS")) || bytes.Contains(b, []byte("Exif")) {
			t.Errorf("размер %d: метаданные не удалены", size)
		}
	}
}

func TestProcessSmallNotUpscaled(t *testing.T) {
	out, err := Processor{}.Process(context.Background(), photo(t, 300, 200), service.ImageOptions{Sizes: sizes})
	if err != nil {
		t.Fatal(err)
	}
	for size, b := range out.Variants {
		if w := decodeWebP(t, b).Bounds().Dx(); w != 300 {
			t.Errorf("%d: %d — маленькое фото не растягиваем", size, w)
		}
	}
}

func TestOrientation(t *testing.T) {
	// 6 — повернуть на 90° по часовой: 400×200 → 200×400, красная половина — сверху
	out, err := Processor{}.Process(context.Background(), withExif(photo(t, 400, 200), 6), service.ImageOptions{Sizes: []int{800}})
	if err != nil {
		t.Fatal(err)
	}
	img := decodeWebP(t, out.Variants[800])
	if img.Bounds().Dx() != 200 || img.Bounds().Dy() != 400 {
		t.Fatalf("не повёрнуто: %v", img.Bounds())
	}
	if r, _, b, _ := img.At(100, 50).RGBA(); r < b {
		t.Errorf("сверху должна быть красная половина")
	}
	for o := 1; o <= 8; o++ {
		got := orient(image.NewNRGBA(image.Rect(0, 0, 4, 2)), o).Bounds()
		want := image.Rect(0, 0, 4, 2)
		if o >= 5 {
			want = image.Rect(0, 0, 2, 4)
		}
		if got != want {
			t.Errorf("ориентация %d: %v", o, got)
		}
	}
	if jpegOrientation([]byte{0xFF, 0xD8, 0xFF}) != 1 || jpegOrientation(photo(t, 2, 2)) != 1 {
		t.Error("без EXIF — 1")
	}
}

func TestCrop(t *testing.T) {
	src := photo(t, 1600, 900)
	// квадрат по центру
	out, err := Processor{}.Process(context.Background(), src, service.ImageOptions{Aspect: 1, Sizes: []int{320}})
	if err != nil || out.Width != 320 || out.Height != 320 {
		t.Fatalf("%v %dx%d", err, out.Width, out.Height)
	}
	// область — левая (красная) половина, 16:9 подгоняется внутри неё
	out, err = Processor{}.Process(context.Background(), src, service.ImageOptions{Aspect: 16.0 / 9, Crop: &service.CropRect{X: 0, Y: 0, W: 0.5, H: 1}, Sizes: []int{800}})
	if err != nil {
		t.Fatal(err)
	}
	img := decodeWebP(t, out.Variants[800])
	b := img.Bounds()
	if ratio := float64(b.Dx()) / float64(b.Dy()); ratio < 1.75 || ratio > 1.8 {
		t.Errorf("пропорции %v", b)
	}
	if r, _, bl, _ := img.At(b.Dx()-5, b.Dy()/2).RGBA(); r < bl {
		t.Error("в кадре должна остаться только красная половина")
	}
}

func TestRejects(t *testing.T) {
	p := Processor{}
	ctx := context.Background()
	if _, err := p.Process(ctx, []byte("GIF89a……"), service.ImageOptions{Sizes: sizes}); !errors.Is(err, service.ErrImageType) {
		t.Errorf("GIF: %v", err)
	}
	if _, err := p.Process(ctx, []byte{0xFF, 0xD8, 0xFF, 0x00, 0x01}, service.ImageOptions{Sizes: sizes}); !errors.Is(err, service.ErrImageType) {
		t.Errorf("битый JPEG: %v", err)
	}
	// «бомба»: заголовок PNG обещает 30000×30000 — отказ до декодирования
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1)))
	b := buf.Bytes()
	binary.BigEndian.PutUint32(b[16:], 30000)
	binary.BigEndian.PutUint32(b[20:], 30000)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	if _, err := p.Process(ctx, b, service.ImageOptions{Sizes: sizes}); !errors.Is(err, service.ErrImageTooLarge) {
		t.Errorf("бомба: %v", err)
	}
	if _, err := (Processor{MaxPixels: 100}).Process(ctx, photo(t, 20, 20), service.ImageOptions{Sizes: sizes}); !errors.Is(err, service.ErrImageTooLarge) {
		t.Errorf("лимит пикселей: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.Process(cctx, photo(t, 20, 20), service.ImageOptions{Sizes: sizes}); !errors.Is(err, context.Canceled) {
		t.Errorf("отменён: %v", err)
	}
}

func TestHEIC(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.heic")
	if err != nil {
		t.Fatal(err)
	}
	out, err := Processor{}.Process(context.Background(), data, service.ImageOptions{Aspect: 1, Sizes: sizes})
	if err != nil {
		t.Fatal(err)
	}
	if out.Width != out.Height || len(out.Variants) != 3 {
		t.Errorf("%dx%d", out.Width, out.Height)
	}
	decodeWebP(t, out.Variants[320])
}

// slow — обработчик, который ждёт сигнала (проверка очереди и параллельности пула).
type slow struct {
	mu      sync.Mutex
	running int
	peak    int
	release chan struct{}
}

func (s *slow) Process(ctx context.Context, _ []byte, _ service.ImageOptions) (service.ProcessedImage, error) {
	s.mu.Lock()
	s.running++
	s.peak = max(s.peak, s.running)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.running--; s.mu.Unlock() }()
	select {
	case <-s.release:
	case <-ctx.Done():
		return service.ProcessedImage{}, ctx.Err()
	}
	return service.ProcessedImage{Width: 1}, nil
}

func TestPool(t *testing.T) {
	s := &slow{release: make(chan struct{})}
	p := NewPool(s, PoolOptions{Workers: 2, Queue: 1, Wait: 50 * time.Millisecond}, nil)
	defer p.Close()
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ { // 2 в работе + 1 в очереди
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Process(ctx, nil, service.ImageOptions{})
			errs <- err
		}()
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := p.Process(ctx, nil, service.ImageOptions{}); !errors.Is(err, service.ErrBusy) {
		t.Errorf("очередь полна — ErrBusy: %v", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := p.Process(cctx, nil, service.ImageOptions{}); err == nil {
		t.Error("клиент ушёл — ошибка")
	}
	close(s.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if s.peak > 2 {
		t.Errorf("параллельно %d > 2", s.peak)
	}
}

type panicky struct{}

func (panicky) Process(context.Context, []byte, service.ImageOptions) (service.ProcessedImage, error) {
	panic("декодер упал")
}

func TestPoolPanic(t *testing.T) {
	p := NewPool(panicky{}, PoolOptions{Workers: 1}, nil)
	defer p.Close()
	if _, err := p.Process(context.Background(), nil, service.ImageOptions{}); !errors.Is(err, service.ErrImageType) {
		t.Errorf("%v", err)
	}
	if _, err := p.Process(context.Background(), nil, service.ImageOptions{}); !errors.Is(err, service.ErrImageType) {
		t.Errorf("воркер жив: %v", err)
	}
}
