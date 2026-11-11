// Package imaging — проверка и перекодирование фото: тип по содержимому (JPEG, PNG, WebP, HEIC), лимит пикселей
// до декодирования (защита от «бомб»), поворот по EXIF, обрезка под пропорции, уменьшение и WebP без метаданных.
// Pool ограничивает параллельность и очередь (декодирование — дорогое по памяти и процессору).
package imaging

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"

	"github.com/gen2brain/heic"
	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"
	xwebp "golang.org/x/image/webp"

	"ohvat/backend/internal/service"
)

// Processor — обработка одного фото (без ограничения параллельности; снаружи — Pool).
type Processor struct {
	MaxPixels int // ширина × высота исходного, 40 млн
	MaxSide   int // длинная сторона исходного, 12 000
	Quality   int // WebP, 82
}

func (p Processor) withDefaults() Processor {
	if p.MaxPixels <= 0 {
		p.MaxPixels = 40_000_000
	}
	if p.MaxSide <= 0 {
		p.MaxSide = 12_000
	}
	if p.Quality <= 0 {
		p.Quality = 82
	}
	return p
}

// Process — проверить и перекодировать. Ошибки формата и размера — service.ErrImageType / ErrImageTooLarge.
func (p Processor) Process(ctx context.Context, data []byte, opt service.ImageOptions) (service.ProcessedImage, error) {
	p = p.withDefaults()
	format := sniff(data)
	if format == "" {
		return service.ProcessedImage{}, service.ErrImageType
	}
	cfg, err := decodeConfig(format, data)
	if err != nil {
		return service.ProcessedImage{}, fmt.Errorf("%w: %v", service.ErrImageType, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > p.MaxSide || cfg.Height > p.MaxSide || cfg.Width*cfg.Height > p.MaxPixels {
		return service.ProcessedImage{}, service.ErrImageTooLarge
	}
	if err := ctx.Err(); err != nil {
		return service.ProcessedImage{}, err
	}
	img, err := decode(format, data)
	if err != nil {
		return service.ProcessedImage{}, fmt.Errorf("%w: %v", service.ErrImageType, err)
	}
	// память: сначала быстро уменьшаем до 2× самого большого варианта (дальше всё — на небольшом кадре),
	// потом поворот по EXIF и обрезка (без копии — подкадр)
	img = prescale(img, 2*maxInt(opt.Sizes))
	if format == formatJPEG {
		img = orient(img, jpegOrientation(data))
	}
	img = crop(img, opt.Aspect, opt.Crop)
	if err := ctx.Err(); err != nil {
		return service.ProcessedImage{}, err
	}

	out := service.ProcessedImage{Variants: make(map[int][]byte, len(opt.Sizes))}
	sizes := append([]int(nil), opt.Sizes...)
	sortDesc(sizes)
	src := img
	for i, size := range sizes {
		dst := fit(src, size) // каждый следующий — из предыдущего: быстрее и без потери качества
		var buf bytes.Buffer
		if err := webp.Encode(&buf, dst, webp.Options{Quality: p.Quality, Method: 4}); err != nil {
			return service.ProcessedImage{}, fmt.Errorf("WebP: %w", err)
		}
		out.Variants[size] = buf.Bytes()
		if i == 0 {
			out.Width, out.Height = dst.Bounds().Dx(), dst.Bounds().Dy()
		}
		src = dst
		if err := ctx.Err(); err != nil {
			return service.ProcessedImage{}, err
		}
	}
	return out, nil
}

func decodeConfig(format string, data []byte) (image.Config, error) {
	r := bytes.NewReader(data)
	switch format {
	case formatJPEG:
		return jpeg.DecodeConfig(r)
	case formatPNG:
		return png.DecodeConfig(r)
	case formatWebP:
		return xwebp.DecodeConfig(r)
	default:
		return heic.DecodeConfig(r)
	}
}

func decode(format string, data []byte) (image.Image, error) {
	r := bytes.NewReader(data)
	switch format {
	case formatJPEG:
		return jpeg.Decode(r)
	case formatPNG:
		return png.Decode(r)
	case formatWebP:
		return xwebp.Decode(r)
	default:
		return heic.Decode(r)
	}
}

// crop — область кадра (доли) и подгонка под пропорции aspect: лишнее срезается поровну с краёв области.
func crop(img image.Image, aspect float64, c *service.CropRect) image.Image {
	b := img.Bounds()
	r := b
	if c != nil {
		r = image.Rect(
			b.Min.X+int(math.Round(c.X*float64(b.Dx()))), b.Min.Y+int(math.Round(c.Y*float64(b.Dy()))),
			b.Min.X+int(math.Round((c.X+c.W)*float64(b.Dx()))), b.Min.Y+int(math.Round((c.Y+c.H)*float64(b.Dy()))),
		).Intersect(b)
		if r.Dx() < 1 || r.Dy() < 1 {
			r = b
		}
	}
	if aspect > 0 {
		w, h := r.Dx(), r.Dy()
		if float64(w)/float64(h) > aspect {
			nw := max(1, int(math.Round(float64(h)*aspect)))
			r.Min.X += (w - nw) / 2
			r.Max.X = r.Min.X + nw
		} else {
			nh := max(1, int(math.Round(float64(w)/aspect)))
			r.Min.Y += (h - nh) / 2
			r.Max.Y = r.Min.Y + nh
		}
	}
	if r == b {
		return img
	}
	if si, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return si.SubImage(r)
	}
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	return out
}

// prescale — грубое быстрое уменьшение очень больших кадров до длинной стороны limit (точное — потом, fit).
func prescale(img image.Image, limit int) image.Image {
	b := img.Bounds()
	if limit <= 0 || max(b.Dx(), b.Dy()) <= limit {
		return img
	}
	k := float64(limit) / float64(max(b.Dx(), b.Dy()))
	out := image.NewNRGBA(image.Rect(0, 0, max(1, int(float64(b.Dx())*k)), max(1, int(float64(b.Dy())*k))))
	draw.ApproxBiLinear.Scale(out, out.Bounds(), img, b, draw.Src, nil)
	return out
}

func maxInt(a []int) int {
	m := 0
	for _, v := range a {
		m = max(m, v)
	}
	return m
}

// fit — уменьшить до длинной стороны size (не увеличивая).
func fit(img image.Image, size int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	long := max(w, h)
	if long <= size {
		return img
	}
	k := float64(size) / float64(long)
	nw, nh := max(1, int(math.Round(float64(w)*k))), max(1, int(math.Round(float64(h)*k)))
	out := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Src, nil)
	return out
}

func sortDesc(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] > a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// Warm — заранее подготовить кодировщик WebP (WASM компилируется при первом вызове, ~1 с): первое фото
// пользователя не ждёт.
func Warm() { webp.Init() }
