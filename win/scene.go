package main

import "math"

// Geometry of the phone back, in image pixels (same as index.html).
const (
	W, H                   = 700, 1330
	BX, BY, BW, BH, BR     = 50.0, 50.0, 600.0, 1230.0, 96.0
	camX, camY, camW, camH = BX + 36, BY + 36, 250.0, 250.0
)

var canvasBg = rgb{0x1e, 0x20, 0x26}

type irect struct{ x0, y0, x1, y1 int }

var full = irect{0, 0, W, H}

func (r irect) empty() bool { return r.x0 >= r.x1 || r.y0 >= r.y1 }

func (r irect) union(o irect) irect {
	if r.empty() {
		return o
	}
	if o.empty() {
		return r
	}
	return irect{min(r.x0, o.x0), min(r.y0, o.y0), max(r.x1, o.x1), max(r.y1, o.y1)}
}

// bounds converts a float box to whole pixels, clipped to the image.
func bounds(x0, y0, x1, y1 float64) irect {
	c := func(v float64, hi int) int { return int(math.Max(0, math.Min(float64(hi), v))) }
	return irect{c(math.Floor(x0), W), c(math.Floor(y0), H), c(math.Ceil(x1), W), c(math.Ceil(y1), H)}
}

// scene holds the static layers drawn around the user's painting.
type scene struct {
	under []uint8 // opaque background, side buttons, drop shadow
	clip  []uint8 // coverage of the case outline (the painting is clipped to it)
	gloss []uint8 // premultiplied shading over the painting; nil when off
	over  []uint8 // premultiplied rim and camera module, always on top
}

func buildScene(caseColor rgb, gloss, export bool) *scene {
	c := newF(W, H)
	if export {
		c.fillAll(white.px(1))
	} else {
		c.fillAll(canvasBg.px(1))
	}
	side := solid(caseColor.shade(-.2).px(1))
	for _, b := range [][4]float64{{BX - 9, BY + 250, 9, 90}, {BX - 9, BY + 370, 9, 90}, {BX + BW, BY + 330, 9, 150}} {
		c.rr(b[0], b[1], b[2], b[3], 4, 0, side)
	}
	if !export {
		const sigma = 22 // canvas shadowBlur 44, offset 20px down
		c.paint(BX-90, BY-70, BX+BW+90, BY+BH+110, func(x, y float64) float64 {
			return .5 * .5 * math.Erfc(sdRoundRect(x, y-20, BX, BY, BW, BH, BR)/(sigma*math.Sqrt2))
		}, solid(px4{a: 1}))
		c.rr(BX, BY, BW, BH, BR, 0, solid(caseColor.px(1)))
	}
	s := &scene{under: c.bytes(), clip: make([]uint8, W*H)}
	cr := bounds(BX-1, BY-1, BX+BW+1, BY+BH+1)
	for y := cr.y0; y < cr.y1; y++ {
		for x := cr.x0; x < cr.x1; x++ {
			s.clip[y*W+x] = uint8(fillCov(sdRoundRect(float64(x)+.5, float64(y)+.5, BX, BY, BW, BH, BR))*255 + .5)
		}
	}
	if gloss {
		g := newF(W, H)
		g.paint(BX, BY, BX+BW, BY+BH, func(float64, float64) float64 { return 1 },
			linear(BX, BY, BX+BW, BY+BH, stop{0, white.px(.22)}, stop{.35, px4{}}, stop{.7, px4{}}, stop{1, px4{a: .2}}))
		s.gloss = g.bytes()
	}
	o := newF(W, H)
	o.rr(BX+2, BY+2, BW-4, BH-4, BR-2, 4, solid(px4{a: .28}))
	camera(o, caseColor)
	s.over = o.bytes()
	return s
}

func camera(c *fcanvas, caseColor rgb) {
	x, y, w, h := camX, camY, camW, camH
	c.rr(x-9, y-9, w+18, h+18, 72, 0, solid(caseColor.shade(-.32).px(1)))
	c.rr(x, y, w, h, 62, 0, linear(x, y, x+w, y+h, stop{0, hexRGB("#2d2e34").px(1)}, stop{1, hexRGB("#0f1013").px(1)}))
	c.rr(x, y, w, h, 62, 3, solid(white.px(.12)))
	lens(c, x+72, y+72, 44)
	lens(c, x+72, y+178, 44)
	lens(c, x+180, y+125, 44)
	c.circle(x+196, y+44, 12, 0, solid(hexRGB("#f4ecd2").px(1)))
	c.circle(x+196, y+44, 12, 3, solid(hexRGB("#17181b").px(1)))
	c.circle(x+196, y+208, 7, 0, solid(hexRGB("#050506").px(1)))
}

func lens(c *fcanvas, cx, cy, r float64) {
	c.circle(cx, cy, r, 0, radial(cx-r*.3, cy-r*.3, r*.1, cx, cy, r, stop{0, hexRGB("#5a5d66").px(1)}, stop{1, hexRGB("#1a1b1f").px(1)}))
	c.circle(cx, cy, r, 3, solid(hexRGB("#0a0a0c").px(1)))
	c.circle(cx, cy, r*.72, 0, radial(cx-r*.25, cy-r*.25, 1, cx, cy, r*.72,
		stop{0, hexRGB("#3d4a7a").px(1)}, stop{.45, hexRGB("#12162a").px(1)}, stop{1, hexRGB("#030308").px(1)}))
	c.circle(cx-r*.28, cy-r*.28, r*.14, 0, solid(white.px(.55)))
}

// composite renders the final image inside r: background, then the case
// colour with the painting and gloss clipped to the case, then the rim and
// camera. dst is BGRA when bgra is set (for GDI), RGBA otherwise.
func (s *scene) composite(dst, art []uint8, cc rgb, r irect, bgra bool) {
	cr, cg, cb := float32(cc.r), float32(cc.g), float32(cc.b)
	ri, bi := 0, 2
	if bgra {
		ri, bi = 2, 0
	}
	for y := r.y0; y < r.y1; y++ {
		for x := r.x0; x < r.x1; x++ {
			i := y*W + x
			o := i * 4
			ur, ug, ub := float32(s.under[o]), float32(s.under[o+1]), float32(s.under[o+2])
			if k := s.clip[i]; k > 0 {
				ia := 1 - float32(art[o+3])/255
				ir, ig, ib := float32(art[o])+cr*ia, float32(art[o+1])+cg*ia, float32(art[o+2])+cb*ia
				if s.gloss != nil {
					ga := 1 - float32(s.gloss[o+3])/255
					ir, ig, ib = float32(s.gloss[o])+ir*ga, float32(s.gloss[o+1])+ig*ga, float32(s.gloss[o+2])+ib*ga
				}
				kf := float32(k) / 255
				ur, ug, ub = ur+(ir-ur)*kf, ug+(ig-ug)*kf, ub+(ib-ub)*kf
			}
			oa := 1 - float32(s.over[o+3])/255
			ur, ug, ub = float32(s.over[o])+ur*oa, float32(s.over[o+1])+ug*oa, float32(s.over[o+2])+ub*oa
			dst[o+ri], dst[o+1], dst[o+bi], dst[o+3] = uint8(ur+.5), uint8(ug+.5), uint8(ub+.5), 255
		}
	}
}
