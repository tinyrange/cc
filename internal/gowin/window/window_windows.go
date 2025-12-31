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

	wmClose       = 0x0010
	wmDestroy     = 0x0002
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmSysKeyDown  = 0x0104
	wmSysKeyUp    = 0x0105
	wmChar        = 0x0102
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	wmMButtonDown = 0x0207
	wmMButtonUp   = 0x0208
	wmXButtonDown = 0x020B
	wmXButtonUp   = 0x020C
	pmRemove      = 0x0001

	// Virtual key codes
	vkShift    = 0x10
	vkControl  = 0x11
	vkMenu     = 0x12 // Alt key
	vkLShift   = 0xA0
	vkRShift   = 0xA1
	vkLControl = 0xA2
	vkRControl = 0xA3
	vkLMenu    = 0xA4
	vkRMenu    = 0xA5
	vkLWin     = 0x5B
	vkRWin     = 0x5C

	// High-order bit mask for GetAsyncKeyState
	keyDownMask = 0x8000

	pfdTypeRGBA      = 0
	pfdMainPlane     = 0
	pfdDrawToWindow  = 0x00000004
	pfdSupportOpenGL = 0x00000020
	pfdDoubleBuffer  = 0x00000001

	cwUseDefault = 0x80000000

	errorClassAlreadyExists = 1410

	// WGL_ARB_create_context constants
	wglContextMajorVersionArb = 0x2091
	wglContextMinorVersionArb = 0x2092
	wglContextFlagsArb        = 0x2094
	// WGL_ARB_create_context_profile constants (when requesting OpenGL 3.2+)
	wglContextProfileMaskArb             = 0x9126
	wglContextCoreProfileBitArb          = 0x00000001
	wglContextCompatibilityProfileBitArb = 0x00000002
	wglContextForwardCompatibleBitArb    = 0x00000002
	wglContextDebugBitArb                = 0x00000001
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
	procGetDpiForWindow  = user32.NewProc("GetDpiForWindow")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
	procMapVirtualKey    = user32.NewProc("MapVirtualKeyW")

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
	hwnd         hwnd
	hdc          hdc
	ctx          hglrc
	running      bool
	keyStates    map[Key]KeyState
	buttonStates map[Button]ButtonState
	inputEvents  []InputEvent
	textInput    string
}

func New(title string, width, height int, useCoreProfile bool) (Window, error) {
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

	ctx, err := createGLContext(hdc, useCoreProfile)
	if err != nil {
		procReleaseDC.Call(uintptr(hwd), uintptr(hdc))
		procDestroyWindow.Call(uintptr(hwd))
		runtime.UnlockOSThread()
		return nil, err
	}

	// Show only after pixel format + context are established.
	procShowWindow.Call(uintptr(hwd), swShow)
	procUpdateWindow.Call(uintptr(hwd))

	win := &winWindow{
		hwnd:         hwd,
		hdc:          hdc,
		ctx:          ctx,
		running:      true,
		keyStates:    make(map[Key]KeyState),
		buttonStates: make(map[Button]ButtonState),
		inputEvents:  make([]InputEvent, 0, 256),
	}
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

	// Transition states: Pressed -> Down, Released -> Up (once per Poll())
	for key, state := range w.keyStates {
		if state == KeyStatePressed {
			w.keyStates[key] = KeyStateDown
		} else if state == KeyStateRepeated {
			w.keyStates[key] = KeyStateDown
		} else if state == KeyStateReleased {
			w.keyStates[key] = KeyStateUp
		}
	}
	for button, state := range w.buttonStates {
		if state == ButtonStatePressed {
			w.buttonStates[button] = ButtonStateDown
		} else if state == ButtonStateReleased {
			w.buttonStates[button] = ButtonStateUp
		}
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
		w.processMessage(&m)
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
	// Try GetDpiForWindow (Windows 10 1607+)
	if procGetDpiForWindow.Find() == nil {
		dpi, _, _ := procGetDpiForWindow.Call(uintptr(w.hwnd))
		if dpi > 0 {
			return float32(dpi) / 96.0
		}
	}
	// Fallback to 1.0 if DPI detection fails
	return 1.0
}

func (w *winWindow) GetKeyState(key Key) KeyState {
	if w.keyStates == nil {
		return KeyStateUp
	}
	if state, ok := w.keyStates[key]; ok {
		return state
	}
	return KeyStateUp
}

func (w *winWindow) GetButtonState(button Button) ButtonState {
	if w.buttonStates == nil {
		return ButtonStateUp
	}
	if state, ok := w.buttonStates[button]; ok {
		return state
	}
	return ButtonStateUp
}

func (w *winWindow) DrainInputEvents() []InputEvent {
	if w == nil || len(w.inputEvents) == 0 {
		return nil
	}
	out := make([]InputEvent, len(w.inputEvents))
	copy(out, w.inputEvents)
	w.inputEvents = w.inputEvents[:0]
	return out
}

func (w *winWindow) TextInput() string {
	s := w.textInput
	w.textInput = ""
	return s
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

func createGLContext(hdc hdc, useCoreProfile bool) (hglrc, error) {
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
	if procAddr != 0 && useCoreProfile {
		// wglCreateContextAttribsARB signature:
		//   HGLRC wglCreateContextAttribsARB(HDC hDC, HGLRC hShareContext, const int *attribList);
		//
		// IMPORTANT: do not try to convert the proc address to a Go func. Call it via SyscallN.
		tryCreate := func(attribs []int32) uintptr {
			if len(attribs) == 0 {
				return 0
			}
			clearLastError()
			r1, _, _ := syscall.SyscallN(
				procAddr,
				uintptr(hdc),
				0, // share context
				uintptr(unsafe.Pointer(&attribs[0])),
			)
			return r1
		}

		// Prefer a GL 3.2+ context (required for GLSL 1.50 / `#version 150`).
		// First try core profile; if that fails, fall back to compatibility, and then
		// to a plain 3.0 context without profile attributes (older drivers).
		candidates := [][]int32{
			{
				wglContextMajorVersionArb, 3,
				wglContextMinorVersionArb, 2,
				wglContextProfileMaskArb, wglContextCoreProfileBitArb,
				wglContextFlagsArb, 0,
				0,
			},
			{
				wglContextMajorVersionArb, 3,
				wglContextMinorVersionArb, 2,
				wglContextProfileMaskArb, wglContextCompatibilityProfileBitArb,
				wglContextFlagsArb, 0,
				0,
			},
			{
				wglContextMajorVersionArb, 3,
				wglContextMinorVersionArb, 0,
				wglContextFlagsArb, 0,
				0,
			},
		}

		var newCtx uintptr
		for _, attribs := range candidates {
			newCtx = tryCreate(attribs)
			if newCtx != 0 {
				break
			}
		}

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
			// Failed to create modern context, use temp context
			finalCtx = hglrc(tempCtx)
		}
	} else {
		// No WGL_ARB_create_context support or legacy requested: use legacy context.
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

// processMessage handles Windows messages for input events.
func (w *winWindow) processMessage(m *msg) {
	switch m.message {
	case wmKeyDown, wmSysKeyDown:
		vk := uint32(m.wParam)
		key := vkToKey(vk)
		if key == KeyUnknown {
			return
		}
		mods := getCurrentMods()
		// Bit 30 of lParam indicates previous key state (1 = was down)
		wasDown := (m.lParam & (1 << 30)) != 0
		isRepeat := wasDown

		w.inputEvents = append(w.inputEvents, InputEvent{
			Type:   InputEventKeyDown,
			Key:    key,
			Repeat: isRepeat,
			Mods:   mods,
		})

		prev := w.GetKeyState(key)
		if isRepeat || prev.IsDown() {
			w.keyStates[key] = KeyStateRepeated
		} else {
			w.keyStates[key] = KeyStatePressed
		}

	case wmKeyUp, wmSysKeyUp:
		vk := uint32(m.wParam)
		key := vkToKey(vk)
		if key == KeyUnknown {
			return
		}
		w.inputEvents = append(w.inputEvents, InputEvent{
			Type: InputEventKeyUp,
			Key:  key,
			Mods: getCurrentMods(),
		})
		w.keyStates[key] = KeyStateReleased

	case wmChar:
		ch := rune(m.wParam)
		// Filter out control characters (except for common ones)
		if ch >= 0x20 && ch != 0x7f {
			s := string(ch)
			w.textInput += s
			w.inputEvents = append(w.inputEvents, InputEvent{
				Type: InputEventText,
				Text: s,
				Mods: getCurrentMods(),
			})
		}

	case wmLButtonDown:
		w.buttonStates[ButtonLeft] = ButtonStatePressed
		w.inputEvents = append(w.inputEvents, InputEvent{
			Type:   InputEventMouseDown,
			Button: ButtonLeft,
			Mods:   getCurrentMods(),
		})

	case wmLButtonUp:
		w.buttonStates[ButtonLeft] = ButtonStateReleased
		w.inputEvents = append(w.inputEvents, InputEvent{
			Type:   InputEventMouseUp,
			Button: ButtonLeft,
			Mods:   getCurrentMods(),
		})

	case wmRButtonDown:
		w.buttonStates[ButtonRight] = ButtonStatePressed
		w.inputEvents = append(w.inputEvents, InputEvent{
			Type:   InputEventMouseDown,
			Button: ButtonRight,
			Mods:   getCurrentMods(),
		})

	case wmRButtonUp:
		w.buttonStates[ButtonRight] = ButtonStateReleased
		w.inputEvents = append(w.inputEvents, InputEvent{
			Type:   InputEventMouseUp,
			Button: ButtonRight,
			Mods:   getCurrentMods(),
		})

	case wmMButtonDown:
		w.buttonStates[ButtonMiddle] = ButtonStatePressed
		w.inputEvents = append(w.inputEvents, InputEvent{
			Type:   InputEventMouseDown,
			Button: ButtonMiddle,
			Mods:   getCurrentMods(),
		})

	case wmMButtonUp:
		w.buttonStates[ButtonMiddle] = ButtonStateReleased
		w.inputEvents = append(w.inputEvents, InputEvent{
			Type:   InputEventMouseUp,
			Button: ButtonMiddle,
			Mods:   getCurrentMods(),
		})

	case wmXButtonDown:
		// XBUTTON1 = 0x0001, XBUTTON2 = 0x0002 in high word
		xbutton := (m.wParam >> 16) & 0xFFFF
		var button Button
		if xbutton == 1 {
			button = Button4
		} else if xbutton == 2 {
			button = Button5
		} else {
			return
		}
		w.buttonStates[button] = ButtonStatePressed
		w.inputEvents = append(w.inputEvents, InputEvent{
			Type:   InputEventMouseDown,
			Button: button,
			Mods:   getCurrentMods(),
		})

	case wmXButtonUp:
		xbutton := (m.wParam >> 16) & 0xFFFF
		var button Button
		if xbutton == 1 {
			button = Button4
		} else if xbutton == 2 {
			button = Button5
		} else {
			return
		}
		w.buttonStates[button] = ButtonStateReleased
		w.inputEvents = append(w.inputEvents, InputEvent{
			Type:   InputEventMouseUp,
			Button: button,
			Mods:   getCurrentMods(),
		})
	}
}

// getCurrentMods returns the current modifier key state.
func getCurrentMods() KeyMods {
	var mods KeyMods
	if procGetAsyncKeyState.Find() == nil {
		if state, _, _ := procGetAsyncKeyState.Call(vkShift); (state & keyDownMask) != 0 {
			mods |= ModShift
		}
		if state, _, _ := procGetAsyncKeyState.Call(vkControl); (state & keyDownMask) != 0 {
			mods |= ModCtrl
		}
		if state, _, _ := procGetAsyncKeyState.Call(vkMenu); (state & keyDownMask) != 0 {
			mods |= ModAlt
		}
		if stateL, _, _ := procGetAsyncKeyState.Call(vkLWin); (stateL & keyDownMask) != 0 {
			mods |= ModSuper
		} else if stateR, _, _ := procGetAsyncKeyState.Call(vkRWin); (stateR & keyDownMask) != 0 {
			mods |= ModSuper
		}
	}
	return mods
}

// vkToKey converts a Windows virtual key code to our Key enum.
func vkToKey(vk uint32) Key {
	switch vk {
	// Letters
	case 0x41:
		return KeyA
	case 0x42:
		return KeyB
	case 0x43:
		return KeyC
	case 0x44:
		return KeyD
	case 0x45:
		return KeyE
	case 0x46:
		return KeyF
	case 0x47:
		return KeyG
	case 0x48:
		return KeyH
	case 0x49:
		return KeyI
	case 0x4A:
		return KeyJ
	case 0x4B:
		return KeyK
	case 0x4C:
		return KeyL
	case 0x4D:
		return KeyM
	case 0x4E:
		return KeyN
	case 0x4F:
		return KeyO
	case 0x50:
		return KeyP
	case 0x51:
		return KeyQ
	case 0x52:
		return KeyR
	case 0x53:
		return KeyS
	case 0x54:
		return KeyT
	case 0x55:
		return KeyU
	case 0x56:
		return KeyV
	case 0x57:
		return KeyW
	case 0x58:
		return KeyX
	case 0x59:
		return KeyY
	case 0x5A:
		return KeyZ

	// Numbers (top row)
	case 0x30:
		return Key0
	case 0x31:
		return Key1
	case 0x32:
		return Key2
	case 0x33:
		return Key3
	case 0x34:
		return Key4
	case 0x35:
		return Key5
	case 0x36:
		return Key6
	case 0x37:
		return Key7
	case 0x38:
		return Key8
	case 0x39:
		return Key9

	// Function keys
	case 0x70:
		return KeyF1
	case 0x71:
		return KeyF2
	case 0x72:
		return KeyF3
	case 0x73:
		return KeyF4
	case 0x74:
		return KeyF5
	case 0x75:
		return KeyF6
	case 0x76:
		return KeyF7
	case 0x77:
		return KeyF8
	case 0x78:
		return KeyF9
	case 0x79:
		return KeyF10
	case 0x7A:
		return KeyF11
	case 0x7B:
		return KeyF12

	// Modifier keys
	case 0xA0: // VK_LSHIFT
		return KeyLeftShift
	case 0xA1: // VK_RSHIFT
		return KeyRightShift
	case 0xA2: // VK_LCONTROL
		return KeyLeftControl
	case 0xA3: // VK_RCONTROL
		return KeyRightControl
	case 0xA4: // VK_LMENU (Left Alt)
		return KeyLeftAlt
	case 0xA5: // VK_RMENU (Right Alt)
		return KeyRightAlt
	case 0x5B: // VK_LWIN
		return KeyLeftSuper
	case 0x5C: // VK_RWIN
		return KeyRightSuper

	// Special keys
	case 0x20: // VK_SPACE
		return KeySpace
	case 0x0D: // VK_RETURN
		return KeyEnter
	case 0x1B: // VK_ESCAPE
		return KeyEscape
	case 0x08: // VK_BACK
		return KeyBackspace
	case 0x2E: // VK_DELETE
		return KeyDelete
	case 0x09: // VK_TAB
		return KeyTab
	case 0x14: // VK_CAPITAL (Caps Lock)
		return KeyCapsLock
	case 0x91: // VK_SCROLL (Scroll Lock)
		return KeyScrollLock
	case 0x90: // VK_NUMLOCK
		return KeyNumLock
	case 0x2C: // VK_SNAPSHOT (Print Screen)
		return KeyPrintScreen
	case 0x13: // VK_PAUSE
		return KeyPause

	// Arrow keys
	case 0x26: // VK_UP
		return KeyUp
	case 0x28: // VK_DOWN
		return KeyDown
	case 0x25: // VK_LEFT
		return KeyLeft
	case 0x27: // VK_RIGHT
		return KeyRight

	// Navigation keys
	case 0x24: // VK_HOME
		return KeyHome
	case 0x23: // VK_END
		return KeyEnd
	case 0x21: // VK_PRIOR (Page Up)
		return KeyPageUp
	case 0x22: // VK_NEXT (Page Down)
		return KeyPageDown
	case 0x2D: // VK_INSERT
		return KeyInsert

	// Punctuation and symbols
	case 0xC0: // VK_OEM_3 (` ~)
		return KeyGraveAccent
	case 0xBD: // VK_OEM_MINUS (- _)
		return KeyMinus
	case 0xBB: // VK_OEM_PLUS (= +)
		return KeyEqual
	case 0xDB: // VK_OEM_4 ([ {)
		return KeyLeftBracket
	case 0xDD: // VK_OEM_6 (] })
		return KeyRightBracket
	case 0xDC: // VK_OEM_5 (\ |)
		return KeyBackslash
	case 0xBA: // VK_OEM_1 (; :)
		return KeySemicolon
	case 0xDE: // VK_OEM_7 (' ")
		return KeyApostrophe
	case 0xBC: // VK_OEM_COMMA (, <)
		return KeyComma
	case 0xBE: // VK_OEM_PERIOD (. >)
		return KeyPeriod
	case 0xBF: // VK_OEM_2 (/ ?)
		return KeySlash

	// Numpad keys
	case 0x60: // VK_NUMPAD0
		return KeyNumpad0
	case 0x61: // VK_NUMPAD1
		return KeyNumpad1
	case 0x62: // VK_NUMPAD2
		return KeyNumpad2
	case 0x63: // VK_NUMPAD3
		return KeyNumpad3
	case 0x64: // VK_NUMPAD4
		return KeyNumpad4
	case 0x65: // VK_NUMPAD5
		return KeyNumpad5
	case 0x66: // VK_NUMPAD6
		return KeyNumpad6
	case 0x67: // VK_NUMPAD7
		return KeyNumpad7
	case 0x68: // VK_NUMPAD8
		return KeyNumpad8
	case 0x69: // VK_NUMPAD9
		return KeyNumpad9
	case 0x6E: // VK_DECIMAL
		return KeyNumpadDecimal
	case 0x6F: // VK_DIVIDE
		return KeyNumpadDivide
	case 0x6A: // VK_MULTIPLY
		return KeyNumpadMultiply
	case 0x6D: // VK_SUBTRACT
		return KeyNumpadSubtract
	case 0x6B: // VK_ADD
		return KeyNumpadAdd
	// Note: Numpad Enter is typically VK_RETURN with extended key flag
	case 0x92: // VK_OEM_NEC_EQUAL (some keyboards)
		return KeyNumpadEqual
	}
	return KeyUnknown
}

// getDisplayScale returns the display scale factor.
// Uses GetDpiForSystem for Windows 10 1607+ or falls back to 1.0.
func getDisplayScale() float32 {
	shcore := syscall.NewLazyDLL("shcore.dll")
	procGetDpiForMonitor := shcore.NewProc("GetDpiForMonitor")

	// Try GetDpiForSystem first (Windows 10 1607+)
	if procGetDpiForMonitor.Find() == nil {
		// We need a monitor handle, so we'll use the primary monitor
		// For simplicity, use GetDC(NULL) approach
		hdc, _, _ := procGetDC.Call(0)
		if hdc != 0 {
			defer procReleaseDC.Call(0, hdc)
			// LOGPIXELSX = 88
			gdi32 := syscall.NewLazyDLL("gdi32.dll")
			procGetDeviceCaps := gdi32.NewProc("GetDeviceCaps")
			if procGetDeviceCaps.Find() == nil {
				dpi, _, _ := procGetDeviceCaps.Call(hdc, 88)
				if dpi > 0 {
					return float32(dpi) / 96.0
				}
			}
		}
	}
	return 1.0
}
