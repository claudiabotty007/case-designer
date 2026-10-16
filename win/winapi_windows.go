//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")

	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procUpdateWindow                  = user32.NewProc("UpdateWindow")
	procMoveWindow                    = user32.NewProc("MoveWindow")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procSendMessageW                  = user32.NewProc("SendMessageW")
	procInvalidateRect                = user32.NewProc("InvalidateRect")
	procBeginPaint                    = user32.NewProc("BeginPaint")
	procEndPaint                      = user32.NewProc("EndPaint")
	procGetClientRect                 = user32.NewProc("GetClientRect")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procSetCapture                    = user32.NewProc("SetCapture")
	procReleaseCapture                = user32.NewProc("ReleaseCapture")
	procSetCursor                     = user32.NewProc("SetCursor")
	procLoadCursorW                   = user32.NewProc("LoadCursorW")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procScreenToClient                = user32.NewProc("ScreenToClient")
	procGetKeyState                   = user32.NewProc("GetKeyState")
	procSetWindowTextW                = user32.NewProc("SetWindowTextW")
	procEnableWindow                  = user32.NewProc("EnableWindow")
	procMessageBoxW                   = user32.NewProc("MessageBoxW")
	procTrackMouseEvent               = user32.NewProc("TrackMouseEvent")
	procSetFocus                      = user32.NewProc("SetFocus")
	procFillRect                      = user32.NewProc("FillRect")
	procFrameRect                     = user32.NewProc("FrameRect")
	procGetSysColorBrush              = user32.NewProc("GetSysColorBrush")
	procSetTimer                      = user32.NewProc("SetTimer")
	procKillTimer                     = user32.NewProc("KillTimer")
	procCreateIconIndirect            = user32.NewProc("CreateIconIndirect")
	procGetDC                         = user32.NewProc("GetDC")
	procReleaseDC                     = user32.NewProc("ReleaseDC")
	procSystemParametersInfoW         = user32.NewProc("SystemParametersInfoW")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem               = user32.NewProc("GetDpiForSystem")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")

	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procCreateBitmap           = gdi32.NewProc("CreateBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procStretchDIBits          = gdi32.NewProc("StretchDIBits")
	procSetStretchBltMode      = gdi32.NewProc("SetStretchBltMode")
	procSetBrushOrgEx          = gdi32.NewProc("SetBrushOrgEx")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procCreateFontW            = gdi32.NewProc("CreateFontW")
	procSetROP2                = gdi32.NewProc("SetROP2")
	procEllipse                = gdi32.NewProc("Ellipse")
	procGetStockObject         = gdi32.NewProc("GetStockObject")
	procGetDeviceCaps          = gdi32.NewProc("GetDeviceCaps")

	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procGetSystemDirectoryW = kernel32.NewProc("GetSystemDirectoryW")
	procCreateActCtxW       = kernel32.NewProc("CreateActCtxW")
	procActivateActCtx      = kernel32.NewProc("ActivateActCtx")

	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")

	procChooseColorW     = comdlg32.NewProc("ChooseColorW")
	procGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
	procGetSaveFileNameW = comdlg32.NewProc("GetSaveFileNameW")
)

// call invokes a Win32 function. uintptrescapes keeps pointer arguments
// (written as uintptr(unsafe.Pointer(&x)) in the call) alive and on the heap
// for the duration of the call, exactly like LazyProc.Call itself.
//
//go:uintptrescapes
func call(p *syscall.LazyProc, args ...uintptr) uintptr {
	r, _, _ := p.Call(args...)
	return r
}

func has(p *syscall.LazyProc) bool { return p.Find() == nil }

func sp(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_CLIPCHILDREN     = 0x02000000
	WS_TABSTOP          = 0x00010000
	WS_GROUP            = 0x00020000

	BS_DEFPUSHBUTTON = 0x1
	BS_AUTOCHECKBOX  = 0x3
	BS_RADIOBUTTON   = 0x4
	BS_PUSHLIKE      = 0x1000
	BM_GETCHECK      = 0xF0
	BM_SETCHECK      = 0xF1
	BST_CHECKED      = 1
	BN_CLICKED       = 0

	SS_LEFT  = 0
	SS_RIGHT = 2

	TBS_NOTICKS     = 0x10
	TBM_GETPOS      = 0x400
	TBM_SETPOS      = 0x405
	TBM_SETRANGEMIN = 0x407
	TBM_SETRANGEMAX = 0x408

	SBARS_SIZEGRIP = 0x100
	SB_SETTEXTW    = 0x40B

	WM_DESTROY        = 0x0002
	WM_SIZE           = 0x0005
	WM_PAINT          = 0x000F
	WM_ERASEBKGND     = 0x0014
	WM_SETCURSOR      = 0x0020
	WM_GETMINMAXINFO  = 0x0024
	WM_SETFONT        = 0x0030
	WM_SETICON        = 0x0080
	WM_KEYDOWN        = 0x0100
	WM_COMMAND        = 0x0111
	WM_TIMER          = 0x0113
	WM_HSCROLL        = 0x0114
	WM_MOUSEMOVE      = 0x0200
	WM_LBUTTONDOWN    = 0x0201
	WM_LBUTTONUP      = 0x0202
	WM_CAPTURECHANGED = 0x0215
	WM_MOUSELEAVE     = 0x02A3
	WM_DPICHANGED     = 0x02E0

	MK_SHIFT  = 0x4
	HTCLIENT  = 1
	TME_LEAVE = 0x2

	IDC_ARROW = 32512
	IDC_CROSS = 32515
	IDC_HAND  = 32649

	SW_HIDE        = 0
	SW_SHOW        = 5
	SW_SHOWDEFAULT = 10

	SWP_NOZORDER   = 0x4
	SWP_NOACTIVATE = 0x10

	SRCCOPY        = 0x00CC0020
	DIB_RGB_COLORS = 0
	HALFTONE       = 4
	R2_NOT         = 6
	R2_COPYPEN     = 13
	NULL_BRUSH     = 5
	LOGPIXELSX     = 88

	COLOR_HIGHLIGHT = 13
	COLOR_BTNFACE   = 15

	FW_NORMAL         = 400
	FW_SEMIBOLD       = 600
	DEFAULT_CHARSET   = 1
	CLEARTYPE_QUALITY = 5

	VK_SHIFT   = 0x10
	VK_CONTROL = 0x11
	VK_MENU    = 0x12
	VK_OEM_4   = 0xDB // [
	VK_OEM_6   = 0xDD // ]

	MB_ICONERROR = 0x10
	ICON_SMALL   = 0
	ICON_BIG     = 1

	SPI_GETWORKAREA = 0x30

	CC_RGBINIT  = 0x1
	CC_FULLOPEN = 0x2

	OFN_OVERWRITEPROMPT = 0x2
	OFN_HIDEREADONLY    = 0x4
	OFN_PATHMUSTEXIST   = 0x800
	OFN_FILEMUSTEXIST   = 0x1000
	OFN_EXPLORER        = 0x80000

	ICC_BAR_CLASSES      = 0x4
	ICC_STANDARD_CLASSES = 0x4000

	ACTCTX_FLAG_ASSEMBLY_DIRECTORY_VALID = 0x4
	ACTCTX_FLAG_RESOURCE_NAME_VALID      = 0x8
	ACTCTX_FLAG_SET_PROCESS_DEFAULT      = 0x10
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct{ x, y int32 }

type rect struct{ left, top, right, bottom int32 }

type msgT struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type paintStruct struct {
	hdc         uintptr
	fErase      int32
	rcPaint     rect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type bitmapInfo struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
	colors          [1]uint32
}

type minMaxInfo struct {
	reserved, maxSize, maxPosition, minTrackSize, maxTrackSize point
}

type trackMouseEvent struct {
	cbSize    uint32
	dwFlags   uint32
	hwndTrack uintptr
	hoverTime uint32
}

type initCommonControlsEx struct{ size, icc uint32 }

type actCtx struct {
	cbSize          uint32
	dwFlags         uint32
	lpSource        *uint16
	processorArch   uint16
	langID          uint16
	assemblyDir     *uint16
	resourceName    uintptr
	applicationName *uint16
	hModule         uintptr
}

type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

type chooseColorT struct {
	lStructSize    uint32
	hwndOwner      uintptr
	hInstance      uintptr
	rgbResult      uint32
	lpCustColors   *[16]uint32
	flags          uint32
	lCustData      uintptr
	lpfnHook       uintptr
	lpTemplateName *uint16
}

type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

func (c rgb) colorref() uintptr { return uintptr(c.r) | uintptr(c.g)<<8 | uintptr(c.b)<<16 }

func fromColorref(v uint32) rgb { return rgb{uint8(v), uint8(v >> 8), uint8(v >> 16)} }

func keyDown(vk uintptr) bool { return int16(call(procGetKeyState, vk)) < 0 }

func clientSize(h uintptr) (int, int) {
	var r rect
	call(procGetClientRect, h, uintptr(unsafe.Pointer(&r)))
	return int(r.right), int(r.bottom)
}

func invalidate(h uintptr) { call(procInvalidateRect, h, 0, 0) }

func setText(h uintptr, s string) { call(procSetWindowTextW, h, uintptr(unsafe.Pointer(sp(s)))) }

func show(h uintptr, on bool) {
	if on {
		call(procShowWindow, h, SW_SHOW)
	} else {
		call(procShowWindow, h, SW_HIDE)
	}
}

func xy(lp uintptr) (int, int) { return int(int16(lp & 0xFFFF)), int(int16(lp >> 16 & 0xFFFF)) }

// enableVisualStyles activates the common-controls v6 manifest that ships
// inside shell32.dll, so buttons, sliders and dialogs get the modern theme
// without the exe needing an embedded manifest resource.
func enableVisualStyles() {
	var dir [260]uint16
	if call(procGetSystemDirectoryW, uintptr(unsafe.Pointer(&dir[0])), 260) == 0 {
		return
	}
	ctx := actCtx{
		dwFlags:      ACTCTX_FLAG_RESOURCE_NAME_VALID | ACTCTX_FLAG_ASSEMBLY_DIRECTORY_VALID | ACTCTX_FLAG_SET_PROCESS_DEFAULT,
		lpSource:     sp("shell32.dll"),
		assemblyDir:  &dir[0],
		resourceName: 124,
	}
	ctx.cbSize = uint32(unsafe.Sizeof(ctx))
	h := call(procCreateActCtxW, uintptr(unsafe.Pointer(&ctx)))
	if h == ^uintptr(0) {
		return
	}
	var cookie uintptr
	call(procActivateActCtx, h, uintptr(unsafe.Pointer(&cookie)))
}

func setDPIAware() {
	if has(procSetProcessDpiAwarenessContext) && call(procSetProcessDpiAwarenessContext, ^uintptr(3)) != 0 { // PER_MONITOR_AWARE_V2
		return
	}
	if has(procSetProcessDPIAware) {
		call(procSetProcessDPIAware)
	}
}

func systemDPI() int {
	if has(procGetDpiForSystem) {
		if d := int(call(procGetDpiForSystem)); d > 0 {
			return d
		}
	}
	dc := call(procGetDC, 0)
	defer call(procReleaseDC, 0, dc)
	if d := int(call(procGetDeviceCaps, dc, LOGPIXELSX)); d > 0 {
		return d
	}
	return 96
}
