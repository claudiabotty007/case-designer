//go:build windows

// Case Studio for Windows: a native Win32 build of the phone-case designer.
// Windows, controls and dialogs are plain Win32; the painting and the case
// are rendered in software (engine.go, scene.go) and drawn with GDI.
package main

import (
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	idUndo = 100 + iota
	idRedo
	idClear
	idPhoto
	idExport
	idColorMore
	idCaseMore
	idFilled
	idGloss
	idTool = 200 // + tool
)

const readyText = "B brush · A airbrush · E eraser · G fill · L line · R rectangle · O ellipse · I picker · [ ] size · Shift constrains shapes"

type slider struct{ label, bar, value uintptr }

// offscreen is a memory DC with a bitmap of a given size.
type offscreen struct {
	dc, bmp, old uintptr
	w, h         int
}

func (o *offscreen) ensure(ref uintptr, w, h int) bool {
	if o.dc != 0 && o.w == w && o.h == h {
		return false
	}
	if o.dc != 0 {
		call(procSelectObject, o.dc, o.old)
		call(procDeleteObject, o.bmp)
		call(procDeleteDC, o.dc)
	}
	o.dc = call(procCreateCompatibleDC, ref)
	o.bmp = call(procCreateCompatibleBitmap, ref, uintptr(w), uintptr(h))
	o.old = call(procSelectObject, o.dc, o.bmp)
	o.w, o.h = w, h
	return true
}

type app struct {
	inst, hwnd, canvas, status uintptr
	dpi                        int
	font, bold                 uintptr
	children, headers          []uintptr

	btn                         map[int]uintptr
	tools                       [nTools]uintptr
	hex, colorMore, caseMore    uintptr
	filled, gloss, hint         uintptr
	size, opacity, tol          slider
	cur, pal, recent, caseSw    *swatches
	presets                     *swatches

	e         *engine
	sc        *scene
	caseColor rgb
	glossOn   bool
	prevTool  tool
	custom    [16]uint32

	frame       []uint8 // composited image, BGRA
	disp        []uint8 // frame scaled to the on-screen size, BGRA
	dbmi        bitmapInfo
	view        struct{ x, y, w, h int } // where the image sits in the canvas window
	mx, my      int
	mouseIn     bool
	tracking    bool
	cache, back offscreen
	stale       bool // cache needs the frame scaled into it again
}

var a *app

func main() {
	runtime.LockOSThread()
	setDPIAware()
	enableVisualStyles()
	icc := initCommonControlsEx{8, ICC_BAR_CLASSES | ICC_STANDARD_CLASSES}
	call(procInitCommonControlsEx, uintptr(unsafe.Pointer(&icc)))

	a = &app{e: newEngine(), caseColor: hexRGB("#f4f1ea"), glossOn: true, btn: map[int]uintptr{}}
	a.inst = call(procGetModuleHandleW, 0)
	a.dpi = systemDPI()
	a.frame = make([]uint8, W*H*4)
	a.rebuild()
	a.create()

	var m msgT
	for int32(call(procGetMessageW, uintptr(unsafe.Pointer(&m)), 0, 0, 0)) > 0 {
		if m.message == WM_KEYDOWN && a.key(m.wParam) {
			continue
		}
		call(procTranslateMessage, uintptr(unsafe.Pointer(&m)))
		call(procDispatchMessageW, uintptr(unsafe.Pointer(&m)))
	}
}

/* ---------- window setup ---------- */

func register(name string, proc func(hwnd, msg, wp, lp uintptr) uintptr, cursor, bg uintptr) {
	wc := wndClassEx{
		lpfnWndProc:   syscall.NewCallback(proc),
		hInstance:     a.inst,
		hCursor:       call(procLoadCursorW, 0, cursor),
		hbrBackground: bg,
		lpszClassName: sp(name),
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	call(procRegisterClassExW, uintptr(unsafe.Pointer(&wc)))
}

func (a *app) s(v int) int { return (v*a.dpi + 48) / 96 }

func (a *app) create() {
	register("CaseStudio", mainProc, IDC_ARROW, COLOR_BTNFACE+1)
	register("CaseStudioCanvas", canvasProc, IDC_ARROW, 0)
	register("CaseStudioSwatches", swatchProc, IDC_ARROW, 0)

	var work rect
	call(procSystemParametersInfoW, SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&work)), 0)
	ww, wh := int(work.right-work.left), int(work.bottom-work.top)
	w, h := min(a.s(1200), ww*95/100), min(a.s(940), wh*95/100)
	x, y := int(work.left)+(ww-w)/2, int(work.top)+(wh-h)/2
	a.hwnd = call(procCreateWindowExW, 0, uintptr(unsafe.Pointer(sp("CaseStudio"))), uintptr(unsafe.Pointer(sp("Case Studio"))),
		WS_OVERLAPPEDWINDOW|WS_CLIPCHILDREN, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0, 0, a.inst, 0)
	if has(procGetDpiForWindow) {
		if d := int(call(procGetDpiForWindow, a.hwnd)); d > 0 {
			a.dpi = d
		}
	}

	a.btn[idUndo] = a.ctl("BUTTON", "Undo", WS_TABSTOP, idUndo)
	a.btn[idRedo] = a.ctl("BUTTON", "Redo", WS_TABSTOP, idRedo)
	a.btn[idClear] = a.ctl("BUTTON", "Clear", WS_TABSTOP, idClear)
	a.btn[idPhoto] = a.ctl("BUTTON", "Place photo…", WS_TABSTOP, idPhoto)
	a.btn[idExport] = a.ctl("BUTTON", "Export JPEG…", WS_TABSTOP|BS_DEFPUSHBUTTON, idExport)
	for i, t := range toolInfo {
		a.tools[i] = a.ctl("BUTTON", t.name+"   "+string(t.key), WS_TABSTOP|BS_RADIOBUTTON|BS_PUSHLIKE, idTool+i)
	}
	call(procSendMessageW, a.tools[toolBrush], BM_SETCHECK, BST_CHECKED, 0)

	a.canvas = a.ctl("CaseStudioCanvas", "", WS_GROUP, 0)

	a.headers = []uintptr{a.ctl("STATIC", "Color", SS_LEFT, 0), a.ctl("STATIC", "Brush", SS_LEFT, 0), a.ctl("STATIC", "Case", SS_LEFT, 0)}
	a.cur = a.newSwatches(1, []rgb{a.e.color}, nil, func(rgb) { a.moreColor() })
	a.colorMore = a.ctl("BUTTON", "More colors…", WS_TABSTOP, idColorMore)
	a.hex = a.ctl("STATIC", a.e.color.hex(), SS_LEFT, 0)
	var pal []rgb
	for _, h := range []string{"#ffffff", "#eceff1", "#cfd4d9", "#aab1b8", "#808890", "#555c64", "#2b3036", "#000000", "#5c3d2e", "#8a5a3b", "#c08a5d", "#ebc9a0"} {
		pal = append(pal, hexRGB(h))
	}
	for _, l := range []float64{86, 70, 56, 42, 28} {
		for _, h := range []float64{0, 16, 34, 50, 90, 145, 172, 198, 220, 258, 292, 330} {
			pal = append(pal, hsl(h, 82, l))
		}
	}
	current := func() rgb { return a.e.color }
	a.pal = a.newSwatches(12, pal, current, a.setColor)
	a.recent = a.newSwatches(10, nil, current, a.setColor)

	a.size = a.newSlider("Size", 1, 150, 24)
	a.opacity = a.newSlider("Opacity", 5, 100, 100)
	a.tol = a.newSlider("Tolerance", 0, 160, 48)
	a.filled = a.ctl("BUTTON", "Filled shape", WS_TABSTOP|BS_AUTOCHECKBOX, idFilled)

	a.caseSw = a.newSwatches(1, []rgb{a.caseColor}, nil, func(rgb) { a.moreCase() })
	a.caseMore = a.ctl("BUTTON", "Case color…", WS_TABSTOP, idCaseMore)
	var presets []rgb
	for _, h := range []string{"#f4f1ea", "#ffffff", "#111214", "#1f3a5f", "#c9a27e", "#f2b8c6", "#a8d5ba", "#c4b5e0"} {
		presets = append(presets, hexRGB(h))
	}
	a.presets = a.newSwatches(8, presets, func() rgb { return a.caseColor }, a.setCase)
	a.gloss = a.ctl("BUTTON", "Glossy shading", WS_TABSTOP|BS_AUTOCHECKBOX, idGloss)
	call(procSendMessageW, a.gloss, BM_SETCHECK, BST_CHECKED, 0)
	a.hint = a.ctl("STATIC", "The camera cutout is part of the case and always stays on top. Export includes the shading and the cutout.\r\n\r\nCtrl+Z undo · Ctrl+Y redo · Ctrl+O photo · Ctrl+S export", SS_LEFT, 0)
	a.status = a.ctl("msctls_statusbar32", "", SBARS_SIZEGRIP, 0)

	a.makeFonts()
	call(procSendMessageW, a.hwnd, WM_SETICON, ICON_BIG, makeIcon(a.s(32)))
	call(procSendMessageW, a.hwnd, WM_SETICON, ICON_SMALL, makeIcon(a.s(16)))
	a.say(readyText)
	a.updateButtons()
	a.layout()
	call(procShowWindow, a.hwnd, SW_SHOWDEFAULT)
	call(procUpdateWindow, a.hwnd)
	call(procSetFocus, a.canvas)
}

func (a *app) ctl(class, text string, style uint32, id int) uintptr {
	h := call(procCreateWindowExW, 0, uintptr(unsafe.Pointer(sp(class))), uintptr(unsafe.Pointer(sp(text))),
		uintptr(WS_CHILD|WS_VISIBLE|style), 0, 0, 0, 0, a.hwnd, uintptr(id), a.inst, 0)
	a.children = append(a.children, h)
	return h
}

func (a *app) newSlider(name string, lo, hi, v int) slider {
	s := slider{label: a.ctl("STATIC", name, SS_LEFT, 0), bar: a.ctl("msctls_trackbar32", "", WS_TABSTOP|TBS_NOTICKS, 0), value: a.ctl("STATIC", "", SS_RIGHT, 0)}
	call(procSendMessageW, s.bar, TBM_SETRANGEMIN, 0, uintptr(lo))
	call(procSendMessageW, s.bar, TBM_SETRANGEMAX, 0, uintptr(hi))
	call(procSendMessageW, s.bar, TBM_SETPOS, 1, uintptr(v))
	return s
}

func (a *app) makeFonts() {
	for _, f := range []uintptr{a.font, a.bold} {
		if f != 0 {
			call(procDeleteObject, f)
		}
	}
	mk := func(weight int) uintptr {
		h := -((9*a.dpi + 36) / 72)
		return call(procCreateFontW, uintptr(h), 0, 0, 0, uintptr(weight), 0, 0, 0, DEFAULT_CHARSET, 0, 0, CLEARTYPE_QUALITY, 0, uintptr(unsafe.Pointer(sp("Segoe UI"))))
	}
	a.font, a.bold = mk(FW_NORMAL), mk(FW_SEMIBOLD)
	for _, h := range a.children {
		call(procSendMessageW, h, WM_SETFONT, a.font, 1)
	}
	for _, h := range a.headers {
		call(procSendMessageW, h, WM_SETFONT, a.bold, 1)
	}
}

func place(h uintptr, x, y, w, ht int) {
	call(procMoveWindow, h, uintptr(x), uintptr(y), uintptr(max(w, 0)), uintptr(max(ht, 0)), 1)
}

func (a *app) layout() {
	if a.status == 0 {
		return
	}
	s := a.s
	cw, ch := clientSize(a.hwnd)
	call(procSendMessageW, a.status, WM_SIZE, 0, 0)
	var sr rect
	call(procGetWindowRect, a.status, uintptr(unsafe.Pointer(&sr)))
	sh := int(sr.bottom - sr.top)

	x, bh := s(10), s(30)
	for _, id := range []int{idUndo, idRedo, idClear} {
		place(a.btn[id], x, s(8), s(76), bh)
		x += s(82)
	}
	place(a.btn[idPhoto], x+s(14), s(8), s(116), bh)
	place(a.btn[idExport], cw-s(12)-s(124), s(8), s(124), bh)
	top := s(8) + bh + s(10)

	tw, th := s(112), s(34)
	for i, h := range a.tools {
		place(h, s(10), top+i*(th+s(4)), tw, th)
	}

	pw := s(276)
	px := cw - pw - s(12)
	cx := s(10) + tw + s(10)
	place(a.canvas, cx, top, px-s(14)-cx, ch-sh-top-s(8))

	y := top
	header := func(h uintptr) { place(h, px, y, pw, s(20)); y += s(24) }
	header(a.headers[0])
	place(a.cur.hwnd, px, y, s(40), s(40))
	place(a.colorMore, px+s(48), y+s(5), s(116), s(30))
	place(a.hex, px+s(174), y+s(11), s(100), s(20))
	y += s(48)
	cell := pw / 12
	place(a.pal.hwnd, px, y, cell*12, cell*(len(a.pal.colors)/12))
	y += cell*(len(a.pal.colors)/12) + s(4)
	place(a.recent.hwnd, px, y, cell*10, cell)
	y += cell + s(16)

	header(a.headers[1])
	row := func(sl slider, on bool) {
		for _, h := range []uintptr{sl.label, sl.bar, sl.value} {
			show(h, on)
		}
		if !on {
			return
		}
		place(sl.label, px, y+s(5), s(66), s(20))
		place(sl.bar, px+s(66), y, pw-s(66)-s(42), s(30))
		place(sl.value, px+pw-s(42), y+s(5), s(42), s(20))
		y += s(34)
	}
	row(a.size, true)
	row(a.opacity, true)
	row(a.tol, a.e.tool == toolFill)
	shapeOpt := a.e.tool == toolRect || a.e.tool == toolEllipse
	show(a.filled, shapeOpt)
	if shapeOpt {
		place(a.filled, px, y, pw, s(24))
		y += s(28)
	}
	y += s(12)

	header(a.headers[2])
	place(a.caseSw.hwnd, px, y, s(40), s(40))
	place(a.caseMore, px+s(48), y+s(5), s(116), s(30))
	y += s(48)
	place(a.presets.hwnd, px, y, s(32)*8, s(32))
	y += s(40)
	place(a.gloss, px, y, pw, s(24))
	y += s(36)
	place(a.hint, px, y, pw, s(120))

	a.syncLabels()
}

func (a *app) syncLabels() {
	setText(a.size.value, itoa(int(a.e.size)))
	setText(a.opacity.value, itoa(int(a.e.opacity*100+.5))+"%")
	setText(a.tol.value, itoa(a.e.tol))
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for neg := v < 0; v != 0; v /= 10 {
		d := v % 10
		if neg {
			d = -d
		}
		b = append([]byte{byte('0' + d)}, b...)
	}
	return string(b)
}

/* ---------- main window ---------- */

func mainProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case WM_SIZE:
		a.layout()
		return 0
	case WM_GETMINMAXINFO:
		mm := (*minMaxInfo)(unsafe.Pointer(lp))
		mm.minTrackSize = point{int32(a.s(900)), int32(a.s(700))}
		return 0
	case WM_DPICHANGED:
		a.dpi = int(wp & 0xFFFF)
		r := (*rect)(unsafe.Pointer(lp))
		a.makeFonts()
		call(procSetWindowPos, hwnd, 0, uintptr(r.left), uintptr(r.top), uintptr(r.right-r.left), uintptr(r.bottom-r.top), SWP_NOZORDER|SWP_NOACTIVATE)
		a.layout()
		return 0
	case WM_COMMAND:
		a.command(int(wp&0xFFFF), int(wp>>16&0xFFFF))
		return 0
	case WM_HSCROLL:
		a.slid(lp)
		return 0
	case WM_TIMER:
		call(procKillTimer, hwnd, wp)
		a.say(readyText)
		return 0
	case WM_DESTROY:
		call(procPostQuitMessage, 0)
		return 0
	}
	return call(procDefWindowProcW, hwnd, msg, wp, lp)
}

func (a *app) command(id, code int) {
	if code != BN_CLICKED {
		return
	}
	switch {
	case id >= idTool && id < idTool+int(nTools):
		a.setTool(tool(id - idTool))
	case id == idUndo:
		a.undo()
	case id == idRedo:
		a.redo()
	case id == idClear:
		a.clear()
	case id == idPhoto:
		a.photo()
	case id == idExport:
		a.export()
	case id == idColorMore:
		a.moreColor()
	case id == idCaseMore:
		a.moreCase()
	case id == idFilled:
		a.e.filled = call(procSendMessageW, a.filled, BM_GETCHECK, 0, 0) == BST_CHECKED
	case id == idGloss:
		a.glossOn = call(procSendMessageW, a.gloss, BM_GETCHECK, 0, 0) == BST_CHECKED
		a.rebuild()
	}
	call(procSetFocus, a.canvas) // keep Space/Enter from re-pressing the button
}

func (a *app) slid(bar uintptr) {
	v := int(call(procSendMessageW, bar, TBM_GETPOS, 0, 0))
	switch bar {
	case a.size.bar:
		a.e.size = float64(v)
		invalidate(a.canvas)
	case a.opacity.bar:
		a.e.opacity = float64(v) / 100
	case a.tol.bar:
		a.e.tol = v
	}
	a.syncLabels()
}

func (a *app) key(vk uintptr) bool {
	if keyDown(VK_MENU) {
		return false
	}
	if keyDown(VK_CONTROL) {
		switch vk {
		case 'Z':
			if keyDown(VK_SHIFT) {
				a.redo()
			} else {
				a.undo()
			}
		case 'Y':
			a.redo()
		case 'S':
			a.export()
		case 'O':
			a.photo()
		default:
			return false
		}
		return true
	}
	for i, t := range toolInfo {
		if vk == uintptr(t.key) {
			a.setTool(tool(i))
			return true
		}
	}
	switch vk {
	case VK_OEM_4:
		a.setSize(a.e.size - 4)
	case VK_OEM_6:
		a.setSize(a.e.size + 4)
	default:
		return false
	}
	return true
}

/* ---------- state changes ---------- */

func (a *app) setTool(t tool) {
	if t == a.e.tool || a.e.drawing {
		return
	}
	a.prevTool, a.e.tool = a.e.tool, t
	for i, h := range a.tools {
		v := uintptr(0)
		if tool(i) == t {
			v = BST_CHECKED
		}
		call(procSendMessageW, h, BM_SETCHECK, v, 0)
	}
	a.layout()
	invalidate(a.canvas)
}

func (a *app) setSize(v float64) {
	a.e.size = max(1, min(150, v))
	call(procSendMessageW, a.size.bar, TBM_SETPOS, 1, uintptr(a.e.size))
	a.syncLabels()
	invalidate(a.canvas)
}

func (a *app) setColor(c rgb) {
	a.e.color = c
	a.cur.colors[0] = c
	setText(a.hex, c.hex())
	for _, s := range []*swatches{a.cur, a.pal, a.recent} {
		invalidate(s.hwnd)
	}
	if a.e.tool == toolEraser {
		a.setTool(toolBrush)
	}
}

func (a *app) addRecent(c rgb) {
	r := []rgb{c}
	for _, x := range a.recent.colors {
		if x != c && len(r) < 10 {
			r = append(r, x)
		}
	}
	a.recent.colors = r
	invalidate(a.recent.hwnd)
}

func (a *app) setCase(c rgb) {
	a.caseColor = c
	a.caseSw.colors[0] = c
	invalidate(a.caseSw.hwnd)
	invalidate(a.presets.hwnd)
	a.rebuild()
}

func (a *app) moreColor() {
	if c, ok := a.chooseColor(a.e.color); ok {
		a.setColor(c)
	}
}

func (a *app) moreCase() {
	if c, ok := a.chooseColor(a.caseColor); ok {
		a.setCase(c)
	}
}

// rebuild re-renders the case layers and the whole frame.
func (a *app) rebuild() {
	a.sc = buildScene(a.caseColor, a.glossOn, false)
	a.e.dirty = full
	a.refresh()
}

// refresh recomposites whatever the engine changed and schedules a repaint.
func (a *app) refresh() {
	r := a.e.takeDirty()
	if r.empty() {
		return
	}
	a.sc.composite(a.frame, a.e.art, a.caseColor, r, true)
	a.scaleRect(r)
	a.stale = true
	if a.canvas != 0 {
		invalidate(a.canvas)
	}
}

func (a *app) updateButtons() {
	en := func(h uintptr, on bool) {
		v := uintptr(0)
		if on {
			v = 1
		}
		call(procEnableWindow, h, v)
	}
	en(a.btn[idUndo], len(a.e.undo) > 0)
	en(a.btn[idRedo], len(a.e.redo) > 0)
}

func (a *app) say(msg string) {
	call(procSendMessageW, a.status, SB_SETTEXTW, 0, uintptr(unsafe.Pointer(sp(msg))))
	if msg != readyText {
		call(procSetTimer, a.hwnd, 1, 4000, 0)
	}
}

func (a *app) fail(msg string) {
	call(procMessageBoxW, a.hwnd, uintptr(unsafe.Pointer(sp(msg))), uintptr(unsafe.Pointer(sp("Case Studio"))), MB_ICONERROR)
}

/* ---------- actions ---------- */

func (a *app) undo() {
	if a.e.undoStep() {
		a.refresh()
	}
	a.updateButtons()
}

func (a *app) redo() {
	if a.e.redoStep() {
		a.refresh()
	}
	a.updateButtons()
}

func (a *app) clear() {
	if a.e.drawing {
		return
	}
	a.e.clearArt()
	a.refresh()
	a.updateButtons()
	a.say("Cleared — Undo brings it back")
}

func (a *app) photo() {
	if a.e.drawing {
		return
	}
	path, ok := a.fileDialog(false, "", "Images (*.png;*.jpg;*.jpeg;*.gif)\x00*.png;*.jpg;*.jpeg;*.gif\x00All files\x00*.*\x00")
	if !ok {
		return
	}
	img, err := loadPhoto(path)
	if err != nil {
		a.fail("Could not read that image.\n\n" + err.Error())
		return
	}
	a.e.placePhoto(img)
	a.refresh()
	a.updateButtons()
	a.say("Photo placed — paint over it freely")
}

func (a *app) export() {
	if a.e.drawing {
		return
	}
	name := time.Now().Format("phone-case-20060102-150405.jpg")
	path, ok := a.fileDialog(true, name, "JPEG image (*.jpg)\x00*.jpg;*.jpeg\x00")
	if !ok {
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	buildScene(a.caseColor, a.glossOn, true).composite(img.Pix, a.e.art, a.caseColor, full, false)
	f, err := os.Create(path)
	if err == nil {
		err = jpeg.Encode(f, img, &jpeg.Options{Quality: 95})
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		a.fail("Could not save the image.\n\n" + err.Error())
		return
	}
	a.say("Saved " + filepath.Base(path))
}

/* ---------- dialogs ---------- */

func (a *app) chooseColor(init rgb) (rgb, bool) {
	cc := chooseColorT{hwndOwner: a.hwnd, rgbResult: uint32(init.colorref()), lpCustColors: &a.custom, flags: CC_RGBINIT | CC_FULLOPEN}
	cc.lStructSize = uint32(unsafe.Sizeof(cc))
	if call(procChooseColorW, uintptr(unsafe.Pointer(&cc))) == 0 {
		return init, false
	}
	return fromColorref(cc.rgbResult), true
}

// fileDialog shows the Open or Save As dialog. filter uses NUL separators.
func (a *app) fileDialog(save bool, name, filter string) (string, bool) {
	buf := make([]uint16, 4096)
	copy(buf, utf16.Encode([]rune(name)))
	flt := utf16.Encode([]rune(filter + "\x00"))
	ofn := openFileName{hwndOwner: a.hwnd, lpstrFilter: &flt[0], lpstrFile: &buf[0], nMaxFile: uint32(len(buf))}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	proc := procGetOpenFileNameW
	ofn.flags = OFN_EXPLORER | OFN_HIDEREADONLY | OFN_PATHMUSTEXIST
	if save {
		proc = procGetSaveFileNameW
		ofn.flags |= OFN_OVERWRITEPROMPT
		ofn.lpstrDefExt = sp("jpg")
	} else {
		ofn.flags |= OFN_FILEMUSTEXIST
	}
	if call(proc, uintptr(unsafe.Pointer(&ofn))) == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

/* ---------- canvas ---------- */

func canvasProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case WM_ERASEBKGND:
		return 1
	case WM_SIZE:
		a.fitView()
		return 0
	case WM_PAINT:
		a.paintCanvas(hwnd)
		return 0
	case WM_SETCURSOR:
		if lp&0xFFFF == HTCLIENT {
			a.setCursor()
			return 1
		}
	case WM_LBUTTONDOWN:
		a.down(xy(lp))
		return 0
	case WM_MOUSEMOVE:
		x, y := xy(lp)
		a.moved(x, y, wp&MK_SHIFT != 0)
		return 0
	case WM_LBUTTONUP, WM_CAPTURECHANGED:
		a.up()
		return 0
	case WM_MOUSELEAVE:
		a.mouseIn, a.tracking = false, false
		invalidate(hwnd)
		return 0
	}
	return call(procDefWindowProcW, hwnd, msg, wp, lp)
}

func (a *app) fitView() {
	cw, ch := clientSize(a.canvas)
	m := float64(a.s(16))
	sc := max(.05, min((float64(cw)-2*m)/W, (float64(ch)-2*m)/H))
	a.view.w, a.view.h = int(W*sc+.5), int(H*sc+.5)
	a.view.x, a.view.y = (cw-a.view.w)/2, (ch-a.view.h)/2
	a.disp = make([]uint8, a.view.w*a.view.h*4)
	a.dbmi = bitmapInfo{biWidth: int32(a.view.w), biHeight: -int32(a.view.h), biPlanes: 1, biBitCount: 32}
	a.dbmi.biSize = 40
	a.scaleRect(full)
	a.stale = true
	invalidate(a.canvas)
}

// scaleRect refreshes the part of disp that shows image rectangle r, averaging
// the source pixels under each screen pixel (and repeating them when zoomed in).
func (a *app) scaleRect(r irect) {
	vw, vh := a.view.w, a.view.h
	if len(a.disp) != vw*vh*4 || vw == 0 || vh == 0 {
		return
	}
	dx0, dx1 := max(0, r.x0*vw/W-1), min(vw, (r.x1*vw+W-1)/W+1)
	dy0, dy1 := max(0, r.y0*vh/H-1), min(vh, (r.y1*vh+H-1)/H+1)
	for dy := dy0; dy < dy1; dy++ {
		sy0 := dy * H / vh
		sy1 := max(sy0+1, (dy+1)*H/vh)
		for dx := dx0; dx < dx1; dx++ {
			sx0 := dx * W / vw
			sx1 := max(sx0+1, (dx+1)*W/vw)
			var sum [3]int
			for sy := sy0; sy < sy1; sy++ {
				row := a.frame[(sy*W+sx0)*4:]
				for k := 0; k < (sx1-sx0)*4; k += 4 {
					sum[0] += int(row[k])
					sum[1] += int(row[k+1])
					sum[2] += int(row[k+2])
				}
			}
			n := (sx1 - sx0) * (sy1 - sy0)
			o := (dy*vw + dx) * 4
			a.disp[o], a.disp[o+1], a.disp[o+2] = uint8(sum[0]/n), uint8(sum[1]/n), uint8(sum[2]/n)
		}
	}
}

func (a *app) toImage(x, y int) (fpt, bool) {
	p := fpt{float64(x-a.view.x) * W / float64(a.view.w), float64(y-a.view.y) * H / float64(a.view.h)}
	return p, p.x >= 0 && p.y >= 0 && p.x < W && p.y < H
}

func (a *app) ringTool() bool { return a.e.tool.freehand() || a.e.tool.shape() }

func (a *app) setCursor() {
	var pt point
	call(procGetCursorPos, uintptr(unsafe.Pointer(&pt)))
	call(procScreenToClient, a.canvas, uintptr(unsafe.Pointer(&pt)))
	_, inside := a.toImage(int(pt.x), int(pt.y))
	switch {
	case inside && a.ringTool():
		call(procSetCursor, 0) // the size ring is the cursor
	case inside:
		call(procSetCursor, call(procLoadCursorW, 0, IDC_CROSS))
	default:
		call(procSetCursor, call(procLoadCursorW, 0, IDC_ARROW))
	}
}

func (a *app) down(x, y int) {
	p, inside := a.toImage(x, y)
	if !inside {
		return
	}
	call(procSetFocus, a.canvas)
	switch a.e.tool {
	case toolPicker:
		if c, ok := a.e.pick(p, a.caseColor); ok {
			a.setColor(c)
		}
		back := a.prevTool
		if back == toolPicker {
			back = toolBrush
		}
		a.setTool(back)
	case toolFill:
		if !a.e.flood(p).empty() {
			a.addRecent(a.e.color)
			a.refresh()
			a.updateButtons()
		}
	default:
		call(procSetCapture, a.canvas)
		a.e.begin(p)
		a.refresh()
	}
}

func (a *app) moved(x, y int, shift bool) {
	a.mx, a.my, a.mouseIn = x, y, true
	if !a.tracking {
		t := trackMouseEvent{dwFlags: TME_LEAVE, hwndTrack: a.canvas}
		t.cbSize = uint32(unsafe.Sizeof(t))
		call(procTrackMouseEvent, uintptr(unsafe.Pointer(&t)))
		a.tracking = true
	}
	if a.e.drawing {
		p, _ := a.toImage(x, y)
		a.e.move(p, shift)
		a.refresh()
	}
	invalidate(a.canvas)
}

func (a *app) up() {
	if !a.e.drawing {
		return
	}
	a.e.end()
	call(procReleaseCapture)
	a.addRecent(a.e.color)
	a.refresh()
	a.updateButtons()
}

func (a *app) paintCanvas(hwnd uintptr) {
	var ps paintStruct
	hdc := call(procBeginPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
	defer call(procEndPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
	w, h := clientSize(hwnd)
	if w <= 0 || h <= 0 {
		return
	}
	if a.cache.ensure(hdc, w, h) {
		a.stale = true
	}
	a.back.ensure(hdc, w, h)
	if a.stale {
		rc := rect{0, 0, int32(w), int32(h)}
		br := call(procCreateSolidBrush, canvasBg.colorref())
		call(procFillRect, a.cache.dc, uintptr(unsafe.Pointer(&rc)), br)
		call(procDeleteObject, br)
		if len(a.disp) > 0 {
			vw, vh := a.view.w, a.view.h
			call(procStretchDIBits, a.cache.dc, uintptr(a.view.x), uintptr(a.view.y), uintptr(vw), uintptr(vh),
				0, 0, uintptr(vw), uintptr(vh), uintptr(unsafe.Pointer(&a.disp[0])), uintptr(unsafe.Pointer(&a.dbmi)), DIB_RGB_COLORS, SRCCOPY)
		}
		a.stale = false
	}
	call(procBitBlt, a.back.dc, 0, 0, uintptr(w), uintptr(h), a.cache.dc, 0, 0, SRCCOPY)
	if _, inside := a.toImage(a.mx, a.my); a.mouseIn && a.ringTool() && (inside || a.e.drawing) {
		d := a.e.size
		if a.e.tool == toolAir {
			d *= 2
		}
		r := max(4, int(d*float64(a.view.w)/W+.5))
		x0, y0 := a.mx-r/2, a.my-r/2
		call(procSetROP2, a.back.dc, R2_NOT)
		old := call(procSelectObject, a.back.dc, call(procGetStockObject, NULL_BRUSH))
		call(procEllipse, a.back.dc, uintptr(x0), uintptr(y0), uintptr(x0+r+1), uintptr(y0+r+1))
		call(procSelectObject, a.back.dc, old)
		call(procSetROP2, a.back.dc, R2_COPYPEN)
	}
	call(procBitBlt, hdc, 0, 0, uintptr(w), uintptr(h), a.back.dc, 0, 0, SRCCOPY)
}

/* ---------- colour swatch grid control ---------- */

type swatches struct {
	hwnd     uintptr
	colors   []rgb
	cols     int
	selected func() rgb // highlighted colour, may be nil
	pick     func(rgb)
}

var swatchOf = map[uintptr]*swatches{}

func (a *app) newSwatches(cols int, colors []rgb, selected func() rgb, pick func(rgb)) *swatches {
	s := &swatches{colors: colors, cols: cols, selected: selected, pick: pick}
	s.hwnd = a.ctl("CaseStudioSwatches", "", 0, 0)
	swatchOf[s.hwnd] = s
	return s
}

func (s *swatches) cell() int {
	w, _ := clientSize(s.hwnd)
	return w / s.cols
}

func swatchProc(hwnd, msg, wp, lp uintptr) uintptr {
	s := swatchOf[hwnd]
	if s != nil {
		switch msg {
		case WM_PAINT:
			s.paint()
			return 0
		case WM_LBUTTONDOWN:
			x, y := xy(lp)
			if c := s.cell(); c > 0 && x/c < s.cols {
				if i := y/c*s.cols + x/c; i < len(s.colors) {
					s.pick(s.colors[i])
				}
			}
			return 0
		case WM_SETCURSOR:
			call(procSetCursor, call(procLoadCursorW, 0, IDC_HAND))
			return 1
		}
	}
	return call(procDefWindowProcW, hwnd, msg, wp, lp)
}

func (s *swatches) paint() {
	var ps paintStruct
	hdc := call(procBeginPaint, s.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer call(procEndPaint, s.hwnd, uintptr(unsafe.Pointer(&ps)))
	w, h := clientSize(s.hwnd)
	all := rect{0, 0, int32(w), int32(h)}
	call(procFillRect, hdc, uintptr(unsafe.Pointer(&all)), call(procGetSysColorBrush, COLOR_BTNFACE))
	c := s.cell()
	g := max(2, c/7)
	edge := call(procCreateSolidBrush, rgb{128, 128, 128}.colorref())
	defer call(procDeleteObject, edge)
	hi := call(procGetSysColorBrush, COLOR_HIGHLIGHT)
	var sel *rgb
	if s.selected != nil {
		v := s.selected()
		sel = &v
	}
	for i, col := range s.colors {
		x, y := i%s.cols*c, i/s.cols*c
		r := rect{int32(x + g), int32(y + g), int32(x + c - g), int32(y + c - g)}
		br := call(procCreateSolidBrush, col.colorref())
		call(procFillRect, hdc, uintptr(unsafe.Pointer(&r)), br)
		call(procDeleteObject, br)
		call(procFrameRect, hdc, uintptr(unsafe.Pointer(&r)), edge)
		if sel != nil && *sel == col {
			for k := int32(1); k <= 2; k++ {
				o := rect{r.left - k, r.top - k, r.right + k, r.bottom + k}
				call(procFrameRect, hdc, uintptr(unsafe.Pointer(&o)), hi)
			}
		}
	}
}

/* ---------- icon ---------- */

func makeIcon(n int) uintptr {
	c := newF(n, n)
	f := float64(n) / 32
	c.rr(7*f, 1.5*f, 18*f, 29*f, 5*f, 0, solid(hexRGB("#ff7a45").px(1)))
	c.rr(9.5*f, 4*f, 8*f, 8*f, 2.5*f, 0, solid(hexRGB("#1d1f25").px(1)))
	pix := c.bytes()
	bgra := make([]uint8, len(pix))
	for i := 0; i < len(pix); i += 4 { // icons want straight (not premultiplied) alpha
		if al := int(pix[i+3]); al > 0 {
			bgra[i], bgra[i+1], bgra[i+2], bgra[i+3] = uint8(int(pix[i+2])*255/al), uint8(int(pix[i+1])*255/al), uint8(int(pix[i])*255/al), uint8(al)
		}
	}
	mask := make([]uint8, n*n)
	color := call(procCreateBitmap, uintptr(n), uintptr(n), 1, 32, uintptr(unsafe.Pointer(&bgra[0])))
	mono := call(procCreateBitmap, uintptr(n), uintptr(n), 1, 1, uintptr(unsafe.Pointer(&mask[0])))
	ii := iconInfo{fIcon: 1, hbmMask: mono, hbmColor: color}
	h := call(procCreateIconIndirect, uintptr(unsafe.Pointer(&ii)))
	call(procDeleteObject, color)
	call(procDeleteObject, mono)
	return h
}
