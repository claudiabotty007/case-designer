package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
)

// loadPhoto decodes a PNG, JPEG or GIF into premultiplied RGBA, turned
// upright according to its EXIF orientation (as browsers do).
func loadPhoto(path string) (*image.RGBA, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	if b.Empty() {
		return nil, image.ErrFormat
	}
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Rect, img, b.Min, draw.Src)
	return orient(src, exifOrientation(data)), nil
}

func exifOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return 1
		}
		m := b[i+1]
		switch {
		case m == 0xFF:
			i++
			continue
		case m == 0x01 || (m >= 0xD0 && m <= 0xD8):
			i += 2
			continue
		case m == 0xD9 || m == 0xDA:
			return 1
		}
		n := int(b[i+2])<<8 | int(b[i+3])
		if n < 2 || i+2+n > len(b) {
			return 1
		}
		if seg := b[i+4 : i+2+n]; m == 0xE1 && len(seg) > 6 && string(seg[:6]) == "Exif\x00\x00" {
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
	if off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off:]))
	for k := 0; k < n; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			break
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			break
		}
	}
	return 1
}

// orient applies an EXIF orientation (1..8) by remapping pixels.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o <= 1 || o > 8 {
		return src
	}
	w, h := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			copy(dst.Pix[(y*dw+x)*4:(y*dw+x)*4+4], src.Pix[sy*src.Stride+sx*4:sy*src.Stride+sx*4+4])
		}
	}
	return dst
}

// shrink averages f×f blocks.
func shrink(src *image.RGBA, f int) *image.RGBA {
	w, h := max(1, src.Rect.Dx()/f), max(1, src.Rect.Dy()/f)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sum [4]int
			n := 0
			for yy := y * f; yy < min((y+1)*f, src.Rect.Dy()); yy++ {
				row := src.Pix[yy*src.Stride:]
				for xx := x * f; xx < min((x+1)*f, src.Rect.Dx()); xx++ {
					for c := 0; c < 4; c++ {
						sum[c] += int(row[xx*4+c])
					}
					n++
				}
			}
			for c := 0; c < 4; c++ {
				dst.Pix[(y*w+x)*4+c] = uint8((sum[c] + n/2) / n)
			}
		}
	}
	return dst
}

func bilinear(src *image.RGBA, x, y float64) [4]float32 {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	x = math.Max(0, math.Min(float64(w-1), x))
	y = math.Max(0, math.Min(float64(h-1), y))
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, w-1), min(y0+1, h-1)
	fx, fy := float32(x-float64(x0)), float32(y-float64(y0))
	p := func(xx, yy, c int) float32 { return float32(src.Pix[yy*src.Stride+xx*4+c]) }
	var out [4]float32
	for c := 0; c < 4; c++ {
		top := p(x0, y0, c) + (p(x1, y0, c)-p(x0, y0, c))*fx
		bot := p(x0, y1, c) + (p(x1, y1, c)-p(x0, y1, c))*fx
		out[c] = top + (bot-top)*fy
	}
	return out
}
