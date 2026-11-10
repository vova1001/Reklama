package imaging

import (
	"bytes"
	"encoding/binary"
)

// Форматы, которые принимаем (определяются по содержимому, не по имени и не по Content-Type).
const (
	formatJPEG = "jpeg"
	formatPNG  = "png"
	formatWebP = "webp"
	formatHEIC = "heic"
)

// sniff — формат по сигнатуре. Анимированные WebP и PNG (APNG), GIF, SVG и всё прочее — "".
func sniff(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return formatJPEG
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		if bytes.Contains(b[:min(len(b), 1<<16)], []byte("acTL")) { // APNG: кадры анимации до данных изображения
			return ""
		}
		return formatPNG
	case len(b) >= 21 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		if bytes.Equal(b[12:16], []byte("VP8X")) && b[20]&0x02 != 0 { // флаг анимации
			return ""
		}
		return formatWebP
	case isHEIF(b):
		return formatHEIC
	}
	return ""
}

// isHEIF — контейнер ISO BMFF с брендом HEIC/HEIF (ftyp в начале файла).
func isHEIF(b []byte) bool {
	if len(b) < 16 || !bytes.Equal(b[4:8], []byte("ftyp")) {
		return false
	}
	size := int(binary.BigEndian.Uint32(b[:4]))
	if size < 16 || size > len(b) || size > 256 {
		size = min(len(b), 64)
	}
	brands := map[string]bool{"heic": true, "heix": true, "heim": true, "heis": true, "hevc": true, "hevx": true, "mif1": true, "msf1": true}
	if brands[string(b[8:12])] {
		return true
	}
	for i := 16; i+4 <= size; i += 4 { // совместимые бренды
		if brands[string(b[i:i+4])] {
			return true
		}
	}
	return false
}
