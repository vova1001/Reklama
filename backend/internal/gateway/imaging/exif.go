package imaging

import (
	"encoding/binary"
	"image"
	"image/draw"
)

// jpegOrientation — тег Orientation (1–8) из EXIF JPEG; нет или не прочитан — 1. Сами метаданные (в том числе
// геометки) в результат не попадают: изображение перекодируется заново, переносится только поворот.
func jpegOrientation(b []byte) int {
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		if marker == 0xDA || marker == 0xD9 { // начало данных — EXIF не было
			return 1
		}
		n := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if n < 2 || i+2+n > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+n]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:8]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	count := int(bo.Uint16(t[off : off+2]))
	for k := 0; k < count; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:e+2]) == 0x0112 {
			v := int(bo.Uint16(t[e+8 : e+10]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// orient — повернуть и отразить по тегу Orientation, чтобы фото с телефона не лежало на боку.
func orient(src image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if o >= 5 {
		w, h = h, w
	}
	in, ok := src.(*image.NRGBA)
	if !ok || b.Min != (image.Point{}) {
		in = image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(in, in.Bounds(), src, b.Min, draw.Src)
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	sw, sh := b.Dx(), b.Dy()
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = sw-1-x, y
			case 3:
				dx, dy = sw-1-x, sh-1-y
			case 4:
				dx, dy = x, sh-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = sh-1-y, x
			case 7:
				dx, dy = sh-1-y, sw-1-x
			case 8:
				dx, dy = y, sw-1-x
			}
			si := in.PixOffset(x, y)
			di := out.PixOffset(dx, dy)
			copy(out.Pix[di:di+4], in.Pix[si:si+4])
		}
	}
	return out
}
