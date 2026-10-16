package main

import (
	"image"
	"math"
)

type tool int

const (
	toolBrush tool = iota
	toolAir
	toolEraser
	toolFill
	toolLine
	toolRect
	toolEllipse
	toolPicker
	nTools
)

var toolInfo = [nTools]struct {
	name string
	key  byte
}{{"Brush", 'B'}, {"Airbrush", 'A'}, {"Eraser", 'E'}, {"Fill", 'G'}, {"Line", 'L'}, {"Rectangle", 'R'}, {"Ellipse", 'O'}, {"Picker", 'I'}}

func (t tool) freehand() bool { return t == toolBrush || t == toolAir || t == toolEraser }
func (t tool) shape() bool    { return t == toolLine || t == toolRect || t == toolEllipse }

type fpt struct{ x, y float64 }

func mid(a, b fpt) fpt             { return fpt{(a.x + b.x) / 2, (a.y + b.y) / 2} }
func dist(a, b fpt) float64        { return math.Hypot(b.x-a.x, b.y-a.y) }
func lerp(a, b fpt, t float64) fpt { return fpt{a.x + (b.x-a.x)*t, a.y + (b.y-a.y)*t} }

// snapshot is the content of a region before an edit, for undo/redo.
type snapshot struct {
	r   irect
	pix []uint8
}

const maxUndo = 50

// engine owns the painting (premultiplied RGBA, W×H) and every edit to it.
// A stroke is painted as a coverage mask over a copy of the painting taken
// when the stroke started, so overlapping parts of one stroke never build up
// opacity (the same model as the in-progress layer in index.html).
type engine struct {
	art, before []uint8
	mask        []float32

	tool    tool
	color   rgb
	size    float64
	opacity float64
	filled  bool
	tol     int

	drawing bool
	pts     []fpt
	start   fpt
	strokeR irect // everything this stroke touched
	shapeR  irect // area of the shape preview currently in the mask
	dirty   irect // changed since the screen was last updated

	undo, redo []snapshot
}

func newEngine() *engine {
	return &engine{
		art: make([]uint8, W*H*4), before: make([]uint8, W*H*4), mask: make([]float32, W*H),
		tool: toolBrush, color: hexRGB("#e63946"), size: 24, opacity: 1, tol: 48,
	}
}

func (e *engine) takeDirty() irect {
	r := e.dirty
	e.dirty = irect{}
	return r
}

func u8(v float32) uint8 { return uint8(v + .5) }

// apply recomputes the painting inside r from the pre-stroke pixels and the mask.
func (e *engine) apply(r irect) {
	op := float32(e.opacity)
	cr, cg, cb := float32(e.color.r), float32(e.color.g), float32(e.color.b)
	erase := e.tool == toolEraser
	for y := r.y0; y < r.y1; y++ {
		for x := r.x0; x < r.x1; x++ {
			i := y*W + x
			o := i * 4
			b, d := e.before[o:o+4:o+4], e.art[o:o+4:o+4]
			m := e.mask[i] * op
			if m == 0 {
				copy(d, b)
				continue
			}
			k := 1 - m
			if erase {
				d[0], d[1], d[2], d[3] = u8(float32(b[0])*k), u8(float32(b[1])*k), u8(float32(b[2])*k), u8(float32(b[3])*k)
			} else {
				d[0], d[1], d[2], d[3] = u8(cr*m+float32(b[0])*k), u8(cg*m+float32(b[1])*k), u8(cb*m+float32(b[2])*k), u8(255*m+float32(b[3])*k)
			}
		}
	}
	e.strokeR = e.strokeR.union(r)
	e.dirty = e.dirty.union(r)
}

/* ---------- strokes ---------- */

func (e *engine) begin(p fpt) {
	e.drawing = true
	copy(e.before, e.art)
	clear(e.mask)
	e.strokeR, e.shapeR = irect{}, irect{}
	e.start = p
	e.pts = append(e.pts[:0], p)
	switch {
	case e.tool == toolAir:
		e.apply(e.dab(p))
	case e.tool.freehand():
		e.apply(e.capsule(p, p, e.size/2))
	}
}

func (e *engine) move(p fpt, shift bool) {
	if !e.drawing {
		return
	}
	if e.tool.shape() {
		e.shape(p, shift)
		return
	}
	prev := e.pts[len(e.pts)-1]
	e.pts = append(e.pts, p)
	n := len(e.pts)
	switch {
	case e.tool == toolAir:
		e.apply(e.airSeg(prev, p))
	case n == 2:
		e.apply(e.capsule(e.pts[0], mid(e.pts[0], e.pts[1]), e.size/2))
	default: // smooth through the midpoints, like quadraticCurveTo in index.html
		e.apply(e.quad(mid(e.pts[n-3], e.pts[n-2]), e.pts[n-2], mid(e.pts[n-2], e.pts[n-1])))
	}
}

func (e *engine) end() {
	if !e.drawing {
		return
	}
	if n := len(e.pts); (e.tool == toolBrush || e.tool == toolEraser) && n >= 2 {
		e.apply(e.capsule(mid(e.pts[n-2], e.pts[n-1]), e.pts[n-1], e.size/2))
	}
	e.drawing = false
	if !e.strokeR.empty() {
		e.pushUndo(snapshot{e.strokeR, e.crop(e.before, e.strokeR)})
	}
}

// capsule stamps a round-capped segment of radius r into the mask.
func (e *engine) capsule(a, b fpt, r float64) irect {
	br := bounds(math.Min(a.x, b.x)-r-1, math.Min(a.y, b.y)-r-1, math.Max(a.x, b.x)+r+1, math.Max(a.y, b.y)+r+1)
	dx, dy := b.x-a.x, b.y-a.y
	ll := dx*dx + dy*dy
	for y := br.y0; y < br.y1; y++ {
		py := float64(y) + .5
		for x := br.x0; x < br.x1; x++ {
			px := float64(x) + .5
			t := 0.0
			if ll > 0 {
				t = clamp01(((px-a.x)*dx + (py-a.y)*dy) / ll)
			}
			k := float32(clamp01(r + .5 - math.Hypot(px-a.x-t*dx, py-a.y-t*dy)))
			if i := y*W + x; k > e.mask[i] {
				e.mask[i] = k
			}
		}
	}
	return br
}

func (e *engine) quad(a, c, b fpt) irect {
	r := e.size / 2
	n := max(1, int(math.Ceil((dist(a, c)+dist(c, b))/math.Max(2, r/3))))
	var br irect
	prev := a
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		p := fpt{u*u*a.x + 2*u*t*c.x + t*t*b.x, u*u*a.y + 2*u*t*c.y + t*t*b.y}
		br = br.union(e.capsule(prev, p, r))
		prev = p
	}
	return br
}

// dab sprays one soft airbrush spot: alpha .2 at the centre fading to 0.
func (e *engine) dab(p fpt) irect {
	r := e.size
	br := bounds(p.x-r, p.y-r, p.x+r, p.y+r)
	for y := br.y0; y < br.y1; y++ {
		for x := br.x0; x < br.x1; x++ {
			d := math.Hypot(float64(x)+.5-p.x, float64(y)+.5-p.y)
			if d >= r {
				continue
			}
			a := float32(.2 * (1 - d/r))
			m := &e.mask[y*W+x]
			*m += a * (1 - *m)
		}
	}
	return br
}

func (e *engine) airSeg(a, b fpt) irect {
	n := max(1, int(math.Ceil(dist(a, b)/math.Max(2, e.size/6))))
	var br irect
	for i := 1; i <= n; i++ {
		br = br.union(e.dab(lerp(a, b, float64(i)/float64(n))))
	}
	return br
}

// shape redraws the line/rectangle/ellipse preview from the start point to b.
func (e *engine) shape(b fpt, shift bool) {
	a := e.start
	dx, dy := b.x-a.x, b.y-a.y
	if shift {
		if e.tool == toolLine {
			ang := math.Round(math.Atan2(dy, dx)/(math.Pi/4)) * (math.Pi / 4)
			l := math.Hypot(dx, dy)
			dx, dy = math.Cos(ang)*l, math.Sin(ang)*l
		} else {
			m := math.Max(math.Abs(dx), math.Abs(dy))
			dx, dy = math.Copysign(m, dx), math.Copysign(m, dy)
		}
	}
	old := e.shapeR
	for y := old.y0; y < old.y1; y++ {
		clear(e.mask[y*W+old.x0 : y*W+old.x1])
	}
	hw := e.size / 2
	var nr irect
	switch e.tool {
	case toolLine:
		nr = e.capsule(a, fpt{a.x + dx, a.y + dy}, hw)
	case toolRect:
		x0, x1 := math.Min(a.x, a.x+dx), math.Max(a.x, a.x+dx)
		y0, y1 := math.Min(a.y, a.y+dy), math.Max(a.y, a.y+dy)
		if e.filled {
			nr = e.cover(x0, y0, x1, y1, func(px, py float64) float64 { return boxCov(px, py, x0, y0, x1, y1) })
		} else { // mitred outline: outer box minus inner box
			hollow := x1-x0 > 2*hw && y1-y0 > 2*hw
			nr = e.cover(x0-hw, y0-hw, x1+hw, y1+hw, func(px, py float64) float64 {
				k := boxCov(px, py, x0-hw, y0-hw, x1+hw, y1+hw)
				if hollow {
					k -= boxCov(px, py, x0+hw, y0+hw, x1-hw, y1-hw)
				}
				return k
			})
		}
	case toolEllipse:
		cx, cy := a.x+dx/2, a.y+dy/2
		rx, ry := math.Max(math.Abs(dx/2), .5), math.Max(math.Abs(dy/2), .5)
		if e.filled {
			nr = e.cover(cx-rx, cy-ry, cx+rx, cy+ry, func(px, py float64) float64 { return fillCov(sdEllipse(px-cx, py-cy, rx, ry)) })
		} else {
			nr = e.cover(cx-rx-hw, cy-ry-hw, cx+rx+hw, cy+ry+hw, func(px, py float64) float64 { return strokeCov(sdEllipse(px-cx, py-cy, rx, ry), e.size) })
		}
	}
	e.shapeR = nr
	e.apply(old.union(nr))
}

func (e *engine) cover(x0, y0, x1, y1 float64, cov func(x, y float64) float64) irect {
	br := bounds(x0-1, y0-1, x1+1, y1+1)
	for y := br.y0; y < br.y1; y++ {
		for x := br.x0; x < br.x1; x++ {
			e.mask[y*W+x] = float32(clamp01(cov(float64(x)+.5, float64(y)+.5)))
		}
	}
	return br
}

/* ---------- one-click tools ---------- */

// flood fills the region around p (scanline), tucking the fill under the
// anti-aliased edge pixels around it so no light fringe is left.
func (e *engine) flood(p fpt) irect {
	x, y := int(math.Floor(p.x)), int(math.Floor(p.y))
	if x < 0 || y < 0 || x >= W || y >= H {
		return irect{}
	}
	// compare un-premultiplied values, like getImageData; before is free scratch here
	d := e.before
	for o := 0; o < len(d); o += 4 {
		a := int(e.art[o+3])
		if a == 0 {
			d[o], d[o+1], d[o+2], d[o+3] = 0, 0, 0, 0
			continue
		}
		d[o], d[o+1], d[o+2], d[o+3] = uint8((int(e.art[o])*255+a/2)/a), uint8((int(e.art[o+1])*255+a/2)/a), uint8((int(e.art[o+2])*255+a/2)/a), uint8(a)
	}
	t := e.tol
	i0 := (y*W + x) * 4
	tr, tg, tb, ta := int(d[i0]), int(d[i0+1]), int(d[i0+2]), int(d[i0+3])
	abs := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	match := func(i int) bool {
		o := i * 4
		return abs(int(d[o])-tr) <= t && abs(int(d[o+1])-tg) <= t && abs(int(d[o+2])-tb) <= t && abs(int(d[o+3])-ta) <= t
	}
	mask := make([]uint8, W*H)
	stack := []int{x, y}
	for len(stack) > 0 {
		cx, cy := stack[len(stack)-2], stack[len(stack)-1]
		stack = stack[:len(stack)-2]
		row := cy * W
		if mask[row+cx] != 0 || !match(row+cx) {
			continue
		}
		l, r := cx, cx
		for l > 0 && mask[row+l-1] == 0 && match(row+l-1) {
			l--
		}
		for r < W-1 && mask[row+r+1] == 0 && match(row+r+1) {
			r++
		}
		for k := l; k <= r; k++ {
			mask[row+k] = 1
		}
		for _, ny := range [2]int{cy - 1, cy + 1} {
			if ny < 0 || ny >= H {
				continue
			}
			run := false
			for k := l; k <= r; k++ {
				j := ny*W + k
				ok := mask[j] == 0 && match(j)
				if ok && !run {
					stack = append(stack, k, ny)
					run = true
				} else if !ok {
					run = false
				}
			}
		}
	}
	// 1 = filled, 2 = translucent edge pixel next to the fill
	bb := irect{W, H, 0, 0}
	for i := range mask {
		px, py := i%W, i/W
		if mask[i] == 0 && d[i*4+3] < 255 &&
			((px > 0 && mask[i-1] == 1) || (px < W-1 && mask[i+1] == 1) || (py > 0 && mask[i-W] == 1) || (py < H-1 && mask[i+W] == 1)) {
			mask[i] = 2
		}
		if mask[i] != 0 {
			bb = irect{min(bb.x0, px), min(bb.y0, py), max(bb.x1, px+1), max(bb.y1, py+1)}
		}
	}
	if bb.empty() {
		return irect{}
	}
	e.pushUndo(snapshot{bb, e.crop(e.art, bb)})
	op := float32(e.opacity)
	src := [4]float32{float32(e.color.r) * op, float32(e.color.g) * op, float32(e.color.b) * op, 255 * op}
	for yy := bb.y0; yy < bb.y1; yy++ {
		for xx := bb.x0; xx < bb.x1; xx++ {
			i := yy*W + xx
			a := e.art[i*4 : i*4+4 : i*4+4]
			switch mask[i] {
			case 1: // source-over
				for c := range a {
					a[c] = u8(src[c] + float32(a[c])*(1-op))
				}
			case 2: // destination-over
				k := 1 - float32(a[3])/255
				for c := range a {
					a[c] = u8(float32(a[c]) + src[c]*k)
				}
			}
		}
	}
	e.dirty = e.dirty.union(bb)
	return bb
}

// pick returns the visible colour at p: the painting over the case colour.
func (e *engine) pick(p fpt, bg rgb) (rgb, bool) {
	x, y := int(math.Floor(p.x)), int(math.Floor(p.y))
	if x < 0 || y < 0 || x >= W || y >= H {
		return rgb{}, false
	}
	o := (y*W + x) * 4
	k := 1 - float64(e.art[o+3])/255
	return rgb{clamp255(float64(e.art[o]) + float64(bg.r)*k), clamp255(float64(e.art[o+1]) + float64(bg.g)*k), clamp255(float64(e.art[o+2]) + float64(bg.b)*k)}, true
}

func (e *engine) clearArt() {
	e.pushUndo(snapshot{full, e.crop(e.art, full)})
	clear(e.art)
	e.dirty = full
}

// placePhoto scales the photo to cover the case and paints it centred.
func (e *engine) placePhoto(src *image.RGBA) {
	s := math.Max(BW/float64(src.Rect.Dx()), BH/float64(src.Rect.Dy()))
	if f := int(1 / s); f >= 2 { // box-shrink first so bilinear sampling never skips pixels
		src = shrink(src, f)
		s = math.Max(BW/float64(src.Rect.Dx()), BH/float64(src.Rect.Dy()))
	}
	w, h := float64(src.Rect.Dx())*s, float64(src.Rect.Dy())*s
	ox, oy := BX+(BW-w)/2, BY+(BH-h)/2
	r := bounds(ox, oy, ox+w, oy+h)
	e.pushUndo(snapshot{r, e.crop(e.art, r)})
	for y := r.y0; y < r.y1; y++ {
		for x := r.x0; x < r.x1; x++ {
			c := bilinear(src, (float64(x)+.5-ox)/s-.5, (float64(y)+.5-oy)/s-.5)
			a := e.art[(y*W+x)*4 : (y*W+x)*4+4 : (y*W+x)*4+4]
			k := 1 - c[3]/255
			for i := range a {
				a[i] = u8(c[i] + float32(a[i])*k)
			}
		}
	}
	e.dirty = e.dirty.union(r)
}

/* ---------- history ---------- */

func (e *engine) crop(src []uint8, r irect) []uint8 {
	w := (r.x1 - r.x0) * 4
	out := make([]uint8, w*(r.y1-r.y0))
	for y := r.y0; y < r.y1; y++ {
		copy(out[(y-r.y0)*w:], src[(y*W+r.x0)*4:(y*W+r.x1)*4])
	}
	return out
}

func (e *engine) paste(r irect, pix []uint8) {
	w := (r.x1 - r.x0) * 4
	for y := r.y0; y < r.y1; y++ {
		copy(e.art[(y*W+r.x0)*4:(y*W+r.x1)*4], pix[(y-r.y0)*w:])
	}
}

func (e *engine) pushUndo(s snapshot) {
	if len(e.undo) == maxUndo {
		e.undo = append(e.undo[:0], e.undo[1:]...)
	}
	e.undo = append(e.undo, s)
	e.redo = nil
}

func (e *engine) step(from, to *[]snapshot) bool {
	n := len(*from)
	if n == 0 || e.drawing {
		return false
	}
	s := (*from)[n-1]
	(*from)[n-1] = snapshot{}
	*from = (*from)[:n-1]
	*to = append(*to, snapshot{s.r, e.crop(e.art, s.r)})
	e.paste(s.r, s.pix)
	e.dirty = e.dirty.union(s.r)
	return true
}

func (e *engine) undoStep() bool { return e.step(&e.undo, &e.redo) }
func (e *engine) redoStep() bool { return e.step(&e.redo, &e.undo) }
