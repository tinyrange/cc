//go:build windows

package window

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/tinyrange/cc/internal/gowin/gl"
)

const (
	csOwnDC   = 0x0020
	csHRedraw = 0x0002
	csVRedraw = 0x0001

	wsOverlappedWindow = 0x00CF0000
	wsClipSiblings     = 0x04000000
	wsClipChildren     = 0x02000000
	swShow             = 5

	wmClose   = 0x0010
	wmDestroy = 0x0002
	pmRemove  = 0x0001

	pfdTypeRGBA      = 0
	pfdMainPlane     = 0
	pfdDrawToWindow  = 0x00000004
	pfdSupportOpenGL = 0x00000020
	pfdDoubleBuffer  = 0x00000001

	cwUseDefault = 0x80000000

	errorClassAlreadyExists = 1410

	// WGL_ARB_create_context constants
	wglContextMajorVersionArb   = 0x2091
	wglContextMinorVersionArb   = 0x2092
	wglContextFlagsArb          = 0x2094
	wglContextCoreProfileBitArb = 0x00000001
)

type (
	hwnd  = syscall.Handle
	hdc   = syscall.Handle
	hglrc = syscall.Handle
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type msg struct {
	hwnd     hwnd
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

type point struct {
	x int32
	y int32
}

type rect struct {
	left   int32
	top    int32
	right  int32
	bottom int32
}

// Mirrors PIXELFORMATDESCRIPTOR (must be 40 bytes).
type pixelFormatDescriptor struct {
	nSize           uint16
	nVersion        uint16
	dwFlags         uint32
	iPixelType      byte
	cColorBits      byte
	cRedBits        byte
	cRedShift       byte
	cGreenBits      byte
	cGreenShift     byte
	cBlueBits       byte
	cBlueShift      byte
	cAlphaBits      byte
	cAlphaShift     byte
	cAccumBits      byte
	cAccumRedBits   byte
	cAccumGreenBits byte
	cAccumBlueBits  byte
	cAccumAlphaBits byte
	cDepthBits      byte
	cStencilBits    byte
	cAuxBuffers     byte
	iLayerType      byte
	bReserved       byte
	dwLayerMask     uint32
	dwVisibleMask   uint32
	dwDamageMask    uint32
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	opengl32 = syscall.NewLazyDLL("opengl32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procPeekMessage      = user32.NewProc("PeekMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procScreenToClient   = user32.NewProc("ScreenToClient")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procWindowFromDC     = user32.NewProc("WindowFromDC")
	procLoadCursor       = user32.NewProc("LoadCursorW")

	procChoosePixelFormat   = gdi32.NewProc("ChoosePixelFormat")
	procDescribePixelFormat = gdi32.NewProc("DescribePixelFormat")
	procGetPixelFormat      = gdi32.NewProc("GetPixelFormat")
	procSetPixelFormat      = gdi32.NewProc("SetPixelFormat")
	procSwapBuffers         = gdi32.NewProc("SwapBuffers")
	procGetObjectType       = gdi32.NewProc("GetObjectType")

	procWglCreateContext  = opengl32.NewProc("wglCreateContext")
	procWglMakeCurrent    = opengl32.NewProc("wglMakeCurrent")
	procWglDeleteContext  = opengl32.NewProc("wglDeleteContext")
	procWglGetProcAddress = opengl32.NewProc("wglGetProcAddress")

	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
	procSetLastError    = kernel32.NewProc("SetLastError")
	procGetLastError    = kernel32.NewProc("GetLastError")
)

func mustFindProc(p *syscall.LazyProc) error {
	if err := p.Find(); err != nil {
		return fmt.Errorf("missing procedure %q: %w", p.Name, err)
	}
	return nil
}

func validateProcs() error {
	procs := []*syscall.LazyProc{
		procRegisterClassEx,
		procCreateWindowEx,
		procGetDC,
		procReleaseDC,
		procDescribePixelFormat,
		procSetPixelFormat,
		procGetPixelFormat,
		procWglCreateContext,
		procWglMakeCurrent,
		procWglDeleteContext,
	}
	for _, p := range procs {
		if err := mustFindProc(p); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	if err := validateProcs(); err != nil {
		panic(err)
	}
}

var (
	// Make the class name unique per-process to avoid CS_OWNDC collisions.
	windowClassName = fmt.Sprintf("GoWin32Window_%d", os.Getpid())
	windowClass     = syscall.StringToUTF16Ptr(windowClassName)

	currentWin *winWindow
)

func lastError() syscall.Errno {
	e, _, _ := procGetLastError.Call()
	return syscall.Errno(e)
}

func clearLastError() {
	procSetLastError.Call(0)
}

func winErr(op string) error {
	e := lastError()
	if e == 0 {
		return fmt.Errorf("%s failed", op)
	}
	return fmt.Errorf("%s failed: %w", op, e)
}

type winWindow struct {
	hwnd    hwnd
	hdc     hdc
	ctx     hglrc
	running bool
}

func New(title string, width, height int, _ bool) (Window, error) {
	runtime.LockOSThread()

	if unsafe.Sizeof(pixelFormatDescriptor{}) != 40 {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf(
			"PIXELFORMATDESCRIPTOR size mismatch: got %d, want 40",
			unsafe.Sizeof(pixelFormatDescriptor{}),
		)
	}

	if err := registerWindowClass(); err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}

	hwd, hdc, err := createWindow(title, width, height)
	if err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}

	// Sanity check: DC belongs to this window.
	clearLastError()
	wfdc, _, _ := procWindowFromDC.Call(uintptr(hdc))
	if hwnd(wfdc) != hwd {
		procReleaseDC.Call(uintptr(hwd), uintptr(hdc))
		procDestroyWindow.Call(uintptr(hwd))
		runtime.UnlockOSThread()
		return nil, fmt.Errorf(
			"HDC does not belong to HWND (WindowFromDC=%#x hwnd=%#x)",
			wfdc,
			uintptr(hwd),
		)
	}

	if _, _, err := chooseAndSetPixelFormat(hdc); err != nil {
		procReleaseDC.Call(uintptr(hwd), uintptr(hdc))
		procDestroyWindow.Call(uintptr(hwd))
		runtime.UnlockOSThread()
		return nil, err
	}

	ctx, err := createGLContext(hdc)
	if err != nil {
		procReleaseDC.Call(uintptr(hwd), uintptr(hdc))
		procDestroyWindow.Call(uintptr(hwd))
		runtime.UnlockOSThread()
		return nil, err
	}

	// Show only after pixel format + context are established.
	procShowWindow.Call(uintptr(hwd), swShow)
	procUpdateWindow.Call(uintptr(hwd))

	win := &winWindow{hwnd: hwd, hdc: hdc, ctx: ctx, running: true}
	currentWin = win

	return win, nil
}

func (w *winWindow) GL() (gl.OpenGL, error) {
	return gl.Load()
}

func (w *winWindow) Close() {
	if w.ctx != 0 {
		procWglMakeCurrent.Call(uintptr(w.hdc), 0)
		procWglDeleteContext.Call(uintptr(w.ctx))
		w.ctx = 0
	}
	if w.hdc != 0 && w.hwnd != 0 {
		procReleaseDC.Call(uintptr(w.hwnd), uintptr(w.hdc))
		w.hdc = 0
	}
	if w.hwnd != 0 {
		procDestroyWindow.Call(uintptr(w.hwnd))
		w.hwnd = 0
	}
	w.running = false
	runtime.UnlockOSThread()
}

func (w *winWindow) Poll() bool {
	if !w.running {
		return false
	}

	var m msg
	for {
		ret, _, _ := procPeekMessage.Call(
			uintptr(unsafe.Pointer(&m)),
			0,
			0,
			0,
			pmRemove,
		)
		if ret == 0 {
			break
		}
		if m.message == wmDestroy {
			w.running = false
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	return w.running
}

func (w *winWindow) Swap() {
	if w.hdc != 0 {
		procSwapBuffers.Call(uintptr(w.hdc))
	}
}

func (w *winWindow) BackingSize() (int, int) {
	var r rect
	procGetClientRect.Call(uintptr(w.hwnd), uintptr(unsafe.Pointer(&r)))
	return int(r.right - r.left), int(r.bottom - r.top)
}

func (w *winWindow) Cursor() (float32, float32) {
	var p point
	ret, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	if ret != 0 {
		procScreenToClient.Call(uintptr(w.hwnd), uintptr(unsafe.Pointer(&p)))
	}
	return float32(p.x), float32(p.y)
}

func (w *winWindow) Scale() float32 {
	// TODO: Implement Windows DPI detection
	return 1.0
}

func (w *winWindow) GetKeyState(key Key) KeyState {
	// TODO: Implement key state tracking
	return KeyStateUp
}

func (w *winWindow) GetButtonState(button Button) ButtonState {
	// TODO: Implement button state tracking
	return ButtonStateUp
}

func (w *winWindow) TextInput() string {
	return ""
}

func registerWindowClass() error {
	cb := syscall.NewCallback(wndProc)
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		style:         csOwnDC | csHRedraw | csVRedraw,
		lpfnWndProc:   cb,
		hInstance:     moduleHandle(),
		hCursor:       loadCursor(),
		hbrBackground: 0,
		lpszClassName: windowClass,
	}

	clearLastError()
	ret, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if ret == 0 {
		// If you ever hit this with the unique name, surface the actual error.
		if errno, ok := err.(syscall.Errno); ok && int(errno) == errorClassAlreadyExists {
			return fmt.Errorf("window class already exists unexpectedly: %s", windowClassName)
		}
		return winErr("RegisterClassExW")
	}
	return nil
}

func createWindow(title string, width, height int) (win hwnd, dc hdc, err error) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)

	style := uint32(wsOverlappedWindow | wsClipSiblings | wsClipChildren)

	clearLastError()
	ret, _, _ := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(windowClass)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(style),
		cwUseDefault,
		cwUseDefault,
		uintptr(width),
		uintptr(height),
		0,
		0,
		uintptr(moduleHandle()),
		0,
	)
	win = hwnd(ret)
	if win == 0 {
		return 0, 0, winErr("CreateWindowExW")
	}

	clearLastError()
	dcRet, _, _ := procGetDC.Call(uintptr(win))
	if dcRet == 0 {
		procDestroyWindow.Call(uintptr(win))
		return 0, 0, winErr("GetDC")
	}

	return win, hdc(dcRet), nil
}

func chooseAndSetPixelFormat(hdc hdc) (int32, pixelFormatDescriptor, error) {
	desired := pixelFormatDescriptor{
		nSize:        uint16(unsafe.Sizeof(pixelFormatDescriptor{})),
		nVersion:     1,
		dwFlags:      pfdDrawToWindow | pfdSupportOpenGL | pfdDoubleBuffer,
		iPixelType:   pfdTypeRGBA,
		cColorBits:   24,
		cDepthBits:   24,
		cStencilBits: 8,
		iLayerType:   pfdMainPlane,
	}

	// Prefer ChoosePixelFormat; then set using the *described* PFD for that index.
	clearLastError()
	pf, _, _ := procChoosePixelFormat.Call(
		uintptr(hdc),
		uintptr(unsafe.Pointer(&desired)),
	)
	if pf == 0 {
		return 0, pixelFormatDescriptor{}, winErr("ChoosePixelFormat")
	}

	var chosen pixelFormatDescriptor
	clearLastError()
	r, _, _ := procDescribePixelFormat.Call(
		uintptr(hdc),
		pf,
		uintptr(unsafe.Sizeof(chosen)),
		uintptr(unsafe.Pointer(&chosen)),
	)
	if r == 0 {
		return 0, pixelFormatDescriptor{}, winErr("DescribePixelFormat")
	}

	const requiredFlags = pfdDrawToWindow | pfdSupportOpenGL | pfdDoubleBuffer
	if (chosen.dwFlags&requiredFlags) != requiredFlags ||
		chosen.iPixelType != pfdTypeRGBA ||
		chosen.cColorBits < 24 {
		// Fallback: strict enumeration to find a usable OpenGL format.
		return enumAndSetPixelFormat(hdc, desired)
	}

	clearLastError()
	ok, _, _ := procSetPixelFormat.Call(
		uintptr(hdc),
		pf,
		uintptr(unsafe.Pointer(&chosen)),
	)
	if ok == 0 {
		return 0, pixelFormatDescriptor{}, fmt.Errorf(
			"SetPixelFormat failed for index %d: %w",
			pf,
			winErr("SetPixelFormat"),
		)
	}

	clearLastError()
	got, _, _ := procGetPixelFormat.Call(uintptr(hdc))
	if got == 0 {
		return 0, pixelFormatDescriptor{}, errors.New(
			"GetPixelFormat returned 0 after SetPixelFormat",
		)
	}
	if got != pf {
		return 0, pixelFormatDescriptor{}, fmt.Errorf(
			"GetPixelFormat mismatch: got=%d want=%d",
			got,
			pf,
		)
	}

	return int32(pf), chosen, nil
}

func enumAndSetPixelFormat(
	hdc hdc,
	desired pixelFormatDescriptor,
) (int32, pixelFormatDescriptor, error) {
	var pfd pixelFormatDescriptor

	clearLastError()
	maxFormats, _, _ := procDescribePixelFormat.Call(
		uintptr(hdc),
		1,
		uintptr(unsafe.Sizeof(pfd)),
		uintptr(unsafe.Pointer(&pfd)),
	)
	if maxFormats == 0 {
		return 0, pixelFormatDescriptor{}, winErr("DescribePixelFormat(count)")
	}

	var chosenFormat uintptr
	var chosenPFD pixelFormatDescriptor

	for i := uintptr(1); i <= maxFormats; i++ {
		clearLastError()
		ret, _, _ := procDescribePixelFormat.Call(
			uintptr(hdc),
			i,
			uintptr(unsafe.Sizeof(pfd)),
			uintptr(unsafe.Pointer(&pfd)),
		)
		if ret == 0 {
			continue
		}

		const requiredFlags = pfdDrawToWindow | pfdSupportOpenGL | pfdDoubleBuffer
		if (pfd.dwFlags & requiredFlags) != requiredFlags {
			continue
		}
		if pfd.iPixelType != pfdTypeRGBA {
			continue
		}
		if pfd.cColorBits < desired.cColorBits {
			continue
		}
		if pfd.cDepthBits < desired.cDepthBits {
			continue
		}
		if pfd.cStencilBits < desired.cStencilBits {
			continue
		}
		if pfd.iLayerType != pfdMainPlane {
			continue
		}

		chosenFormat = i
		chosenPFD = pfd
		break
	}

	if chosenFormat == 0 {
		return 0, pixelFormatDescriptor{}, errors.New(
			"failed to find a suitable OpenGL pixel format",
		)
	}

	clearLastError()
	ok, _, _ := procSetPixelFormat.Call(
		uintptr(hdc),
		chosenFormat,
		uintptr(unsafe.Pointer(&chosenPFD)),
	)
	if ok == 0 {
		return 0, pixelFormatDescriptor{}, winErr("SetPixelFormat(enum)")
	}

	clearLastError()
	got, _, _ := procGetPixelFormat.Call(uintptr(hdc))
	if got == 0 {
		return 0, pixelFormatDescriptor{}, errors.New(
			"GetPixelFormat returned 0 after SetPixelFormat (enum path)",
		)
	}

	return int32(chosenFormat), chosenPFD, nil
}

func createGLContext(hdc hdc) (hglrc, error) {
	// First create a temporary legacy context to bootstrap
	clearLastError()
	tempCtx, _, _ := procWglCreateContext.Call(uintptr(hdc))
	if tempCtx == 0 {
		return 0, winErr("wglCreateContext (temp)")
	}

	clearLastError()
	ret, _, _ := procWglMakeCurrent.Call(uintptr(hdc), tempCtx)
	if ret == 0 {
		procWglDeleteContext.Call(tempCtx)
		return 0, winErr("wglMakeCurrent (temp)")
	}

	// Try to load wglCreateContextAttribsARB
	var finalCtx hglrc
	procName := syscall.StringBytePtr("wglCreateContextAttribsARB")
	procAddr, _, _ := procWglGetProcAddress.Call(uintptr(unsafe.Pointer(procName)))
	if procAddr != 0 {
		// We have WGL_ARB_create_context support
		createContextAttribsARB := *(*func(uintptr, uintptr, uintptr, *int32) uintptr)(unsafe.Pointer(&procAddr))

		attribs := []int32{
			wglContextMajorVersionArb, 3,
			wglContextMinorVersionArb, 0,
			wglContextFlagsArb, wglContextCoreProfileBitArb,
			0,
		}

		clearLastError()
		newCtx := createContextAttribsARB(uintptr(hdc), 0, uintptr(unsafe.Pointer(&attribs[0])), nil)
		if newCtx != 0 {
			// Make the new context current
			clearLastError()
			ret, _, _ := procWglMakeCurrent.Call(uintptr(hdc), newCtx)
			if ret != 0 {
				// Success! Delete temp context
				procWglDeleteContext.Call(tempCtx)
				finalCtx = hglrc(newCtx)
			} else {
				// Failed to make new context current, clean up and fall back
				procWglDeleteContext.Call(newCtx)
				finalCtx = hglrc(tempCtx)
			}
		} else {
			// Failed to create GL 3.0 context, use temp context
			finalCtx = hglrc(tempCtx)
		}
	} else {
		// No WGL_ARB_create_context support, use legacy context
		finalCtx = hglrc(tempCtx)
	}

	if finalCtx == 0 {
		return 0, errors.New("failed to create OpenGL context")
	}

	return finalCtx, nil
}

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmClose:
		current := currentWin
		if current != nil && current.hwnd == syscall.Handle(hwnd) {
			current.running = false
		}
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return ret
}

func loadCursor() syscall.Handle {
	const idcArrow = 32512
	clearLastError()
	ret, _, _ := procLoadCursor.Call(0, uintptr(idcArrow))
	return syscall.Handle(ret)
}

func moduleHandle() syscall.Handle {
	clearLastError()
	h, _, _ := procGetModuleHandle.Call(0)
	return syscall.Handle(h)
}

// getDisplayScale returns the display scale factor.
// TODO: Implement Windows DPI detection using GetDpiForSystem or similar.
func getDisplayScale() float32 {
	return 1.0
}
