package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

/* ---------- colours ---------- */

type rgb struct{ r, g, b uint8 }

func hexRGB(s string) rgb {
	v, _ := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	return rgb{uint8(v >> 16), uint8(v >> 8), uint8(v)}
}

func (c rgb) hex() string { return fmt.Sprintf("#%02X%02X%02X", c.r, c.g, c.b) }

func clamp255(v float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(v)))) }

// shade mixes the colour towards white (amt > 0) or black (amt < 0).
func (c rgb) shade(amt float64) rgb {
	t, p := 255.0, math.Abs(amt)
	if amt < 0 {
		t = 0
	}
	f := func(v uint8) uint8 { return clamp255(float64(v) + (t-float64(v))*p) }
	return rgb{f(c.r), f(c.g), f(c.b)}
}

func hsl(h, s, l float64) rgb {
	s, l = s/100, l/100
	a := s * math.Min(l, 1-l)
	f := func(n float64) uint8 {
		k := math.Mod(n+h/30, 12)
		return clamp255((l - a*math.Max(-1, math.Min(k-3, math.Min(9-k, 1)))) * 255)
	}
	return rgb{f(0), f(8), f(4)}
}

// px4 is a premultiplied RGBA colour with components in 0..1.
type px4 struct{ r, g, b, a float32 }

func (c rgb) px(alpha float64) px4 {
	f := float32(alpha) / 255
	return px4{float32(c.r) * f, float32(c.g) * f, float32(c.b) * f, float32(alpha)}
}

var white = rgb{255, 255, 255}

/* ---------- anti-aliased shapes from signed distances ---------- */

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func fillCov(d float64) float64 { return clamp01(0.5 - d) }

func strokeCov(d, lw float64) float64 { return clamp01(lw/2 + 0.5 - math.Abs(d)) }

func sdRoundRect(px, py, x, y, w, h, r float64) float64 {
	hx, hy := w/2, h/2
	r = math.Min(r, math.Min(hx, hy))
	qx := math.Abs(px-x-hx) - (hx - r)
	qy := math.Abs(py-y-hy) - (hy - r)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

func sdCircle(px, py, cx, cy, r float64) float64 { return math.Hypot(px-cx, py-cy) - r }

// sdEllipse approximates the signed distance to an axis-aligned ellipse
// centred on the origin.
func sdEllipse(x, y, rx, ry float64) float64 {
	x, y = math.Abs(x), math.Abs(y)
	k1 := math.Hypot(x/(rx*rx), y/(ry*ry))
	if k1 == 0 {
		return -math.Min(rx, ry)
	}
	k0 := math.Hypot(x/rx, y/ry)
	return k0 * (k0 - 1) / k1
}

// boxCov is the exact area of pixel (px,py) covered by the box.
func boxCov(px, py, x0, y0, x1, y1 float64) float64 {
	return clamp01(math.Min(px+.5, x1)-math.Max(px-.5, x0)) * clamp01(math.Min(py+.5, y1)-math.Max(py-.5, y0))
}

/* ---------- gradients (same semantics as canvas) ---------- */

type paintFn func(x, y float64) px4

func solid(c px4) paintFn { return func(float64, float64) px4 { return c } }

type stop struct {
	t float64
	c px4
}

func ramp(stops []stop, t float64) px4 {
	if t <= stops[0].t {
		return stops[0].c
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].t {
			a, b := stops[i-1].c, stops[i].c
			f := float32((t - stops[i-1].t) / (stops[i].t - stops[i-1].t))
			return px4{a.r + (b.r-a.r)*f, a.g + (b.g-a.g)*f, a.b + (b.b-a.b)*f, a.a + (b.a-a.a)*f}
		}
	}
	return stops[len(stops)-1].c
}

func linear(x0, y0, x1, y1 float64, stops ...stop) paintFn {
	dx, dy := x1-x0, y1-y0
	l := dx*dx + dy*dy
	return func(x, y float64) px4 { return ramp(stops, ((x-x0)*dx+(y-y0)*dy)/l) }
}

// radial is a two-circle gradient like createRadialGradient: t is the largest
// ω for which the point lies on the circle interpolated between the two.
func radial(x0, y0, r0, x1, y1, r1 float64, stops ...stop) paintFn {
	cdx, cdy, dr := x1-x0, y1-y0, r1-r0
	a := cdx*cdx + cdy*cdy - dr*dr
	return func(x, y float64) px4 {
		pdx, pdy := x-x0, y-y0
		b := pdx*cdx + pdy*cdy + r0*dr
		c := pdx*pdx + pdy*pdy - r0*r0
		w := math.Inf(-1)
		if math.Abs(a) < 1e-9 {
			if b != 0 {
				w = c / (2 * b)
			}
		} else if disc := b*b - a*c; disc >= 0 {
			s := math.Sqrt(disc)
			for _, v := range [2]float64{(b + s) / a, (b - s) / a} {
				if v > w && r0+v*dr >= 0 {
					w = v
				}
			}
		}
		if math.IsInf(w, -1) || r0+w*dr < 0 {
			return px4{}
		}
		return ramp(stops, w)
	}
}

/* ---------- float canvas used to build the static case layers ---------- */

type fcanvas struct {
	w, h int
	p    []px4
}

func newF(w, h int) *fcanvas { return &fcanvas{w, h, make([]px4, w*h)} }

func (c *fcanvas) fillAll(v px4) {
	for i := range c.p {
		c.p[i] = v
	}
}

// paint composites col over the canvas with the given per-pixel coverage,
// visiting only pixels inside the bounding box.
func (c *fcanvas) paint(x0, y0, x1, y1 float64, cov func(x, y float64) float64, col paintFn) {
	ix0, iy0 := max(0, int(math.Floor(x0))), max(0, int(math.Floor(y0)))
	ix1, iy1 := min(c.w, int(math.Ceil(x1))), min(c.h, int(math.Ceil(y1)))
	for y := iy0; y < iy1; y++ {
		fy := float64(y) + .5
		for x := ix0; x < ix1; x++ {
			fx := float64(x) + .5
			k := cov(fx, fy)
			if k <= 0 {
				continue
			}
			s, kf := col(fx, fy), float32(math.Min(k, 1))
			d := &c.p[y*c.w+x]
			ia := 1 - s.a*kf
			d.r = s.r*kf + d.r*ia
			d.g = s.g*kf + d.g*ia
			d.b = s.b*kf + d.b*ia
			d.a = s.a*kf + d.a*ia
		}
	}
}

// rr fills (lw == 0) or strokes a rounded rectangle.
func (c *fcanvas) rr(x, y, w, h, r, lw float64, col paintFn) {
	m := lw/2 + 1
	c.paint(x-m, y-m, x+w+m, y+h+m, func(px, py float64) float64 {
		d := sdRoundRect(px, py, x, y, w, h, r)
		if lw == 0 {
			return fillCov(d)
		}
		return strokeCov(d, lw)
	}, col)
}

// circle fills (lw == 0) or strokes a circle.
func (c *fcanvas) circle(cx, cy, r, lw float64, col paintFn) {
	m := r + lw/2 + 1
	c.paint(cx-m, cy-m, cx+m, cy+m, func(px, py float64) float64 {
		d := sdCircle(px, py, cx, cy, r)
		if lw == 0 {
			return fillCov(d)
		}
		return strokeCov(d, lw)
	}, col)
}

// bytes quantises the canvas to premultiplied 8-bit RGBA.
func (c *fcanvas) bytes() []uint8 {
	out := make([]uint8, len(c.p)*4)
	for i, v := range c.p {
		out[i*4] = uint8(v.r*255 + .5)
		out[i*4+1] = uint8(v.g*255 + .5)
		out[i*4+2] = uint8(v.b*255 + .5)
		out[i*4+3] = uint8(v.a*255 + .5)
	}
	return out
}
