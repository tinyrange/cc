//go:build darwin

// Package darwin implements a CGO-free Cocoa + NSOpenGL bootstrap using purego.
// It keeps control of the run loop so callers can drive rendering manually.
package window

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/tinyrange/cc/internal/gowin/gl"
)

// NS geometry mirrors (keep alignment explicit).
type NSPoint struct {
	X float64
	Y float64
}

type NSSize struct {
	W float64
	H float64
}

type NSRect struct {
	Origin NSPoint
	Size   NSSize
}

// Cocoa constants (subset).
const (
	nsApplicationActivationPolicyRegular = 0

	nsWindowStyleTitled      = 1 << 0
	nsWindowStyleClosable    = 1 << 1
	nsWindowStyleMiniaturize = 1 << 2
	nsWindowStyleResizable   = 1 << 3

	nsBackingStoreBuffered = 2

	nsEventMaskAny = ^uint(0)

	// NSOpenGL pixel format attributes.
	nsOpenGLPFAAccelerated       = 73
	nsOpenGLPFADoubleBuffer      = 5
	nsOpenGLPFAColorSize         = 8
	nsOpenGLPFADepthSize         = 12
	nsOpenGLPFAOpenGLProfile     = 99
	nsOpenGLProfileVersionLegacy = 0x1000
	nsOpenGLProfileVersion41Core = 0x4100

	nsOpenGLCPSwapInterval = 222
)

// Cocoa exposes objects as pointers (Objective-C id).
type Cocoa struct {
	app     objc.ID
	window  objc.ID
	view    objc.ID
	ctx     objc.ID
	pool    objc.ID
	running bool
}

var (
	initOnce sync.Once
	initErr  error

	// CoreFoundation.
	cfRunLoopRunInMode func(uintptr, float64, bool) int32
	cfDefaultMode      uintptr

	// Cached selectors.
	selAlloc                 objc.SEL
	selInit                  objc.SEL
	selRelease               objc.SEL
	selSharedApplication     objc.SEL
	selNextEventMatchingMask objc.SEL
	selSetActivationPolicy   objc.SEL
	selFinishLaunching       objc.SEL
	selStringWithUTF8String  objc.SEL
	selInitWithContentRect   objc.SEL
	selMakeKeyAndOrderFront  objc.SEL
	selSetTitle              objc.SEL
	selSetAcceptsMouseMoved  objc.SEL
	selSetReleasedWhenClosed objc.SEL
	selCenter                objc.SEL
	selContentView           objc.SEL
	selBounds                objc.SEL
	selMouseLocationOutside  objc.SEL
	selConvertRectToBacking  objc.SEL
	selIsVisible             objc.SEL
	selSendEvent             objc.SEL
	selFlushBuffer           objc.SEL
	selSetView               objc.SEL
	selMakeCurrentContext    objc.SEL
	selClearCurrentContext   objc.SEL
	selInitWithAttributes    objc.SEL
	selInitWithFormat        objc.SEL
	selSetValuesForParameter objc.SEL
)

// Init boots Cocoa and OpenGL, keeping control of the run loop in Go.
func New(title string, width, height int, useCoreProfile bool) (Window, error) {
	runtime.LockOSThread()
	if err := ensureRuntime(); err != nil {
		return nil, err
	}

	c := &Cocoa{running: true}
	if err := c.bootstrapApp(); err != nil {
		return nil, err
	}
	if err := c.makeWindow(title, width, height); err != nil {
		return nil, err
	}
	if err := c.makeGLContext(useCoreProfile); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cocoa) GL() (gl.OpenGL, error) {
	return gl.Load()
}

// Poll pumps Cocoa events once. Returns false when the window is no longer visible.
func (c *Cocoa) Poll() bool {
	if !c.running {
		return false
	}

	// Drain one slice of the run loop without blocking and pump pending NSEvents.
	cfRunLoopRunInMode(cfDefaultMode, 0, true)
	for {
		ev := objc.Send[objc.ID](c.app, selNextEventMatchingMask, nsEventMaskAny, objc.ID(0), objc.ID(cfDefaultMode), true)
		if ev == 0 {
			break
		}
		c.app.Send(selSendEvent, ev)
	}

	if !objc.Send[bool](c.window, selIsVisible) {
		c.running = false
	}
	return c.running
}

// Swap presents the back buffer.
func (c *Cocoa) Swap() {
	if c.ctx != 0 {
		c.ctx.Send(selFlushBuffer)
	}
}

// BackingSize returns the current pixel dimensions, accounting for Retina scale.
func (c *Cocoa) BackingSize() (int, int) {
	if c.view == 0 {
		return 0, 0
	}
	bounds := objc.Send[NSRect](c.view, selBounds)
	backing := objc.Send[NSRect](c.view, selConvertRectToBacking, bounds)
	return int(backing.Size.W), int(backing.Size.H)
}

// Cursor returns the mouse position in backing pixel coordinates.
func (c *Cocoa) Cursor() (float32, float32) {
	_, h := c.BackingSize()
	x, y := c.cursorBackingPos()
	return x, float32(h) - y
}

// Close tears down the GL context and window.
func (c *Cocoa) Close() {
	if c.ctx != 0 {
		objc.ID(objc.GetClass("NSOpenGLContext")).Send(selClearCurrentContext)
		c.ctx.Send(selRelease)
		c.ctx = 0
	}
	if c.window != 0 {
		c.window.Send(selRelease)
		c.window = 0
	}
	if c.pool != 0 {
		c.pool.Send(selRelease)
		c.pool = 0
	}
	c.running = false
	runtime.UnlockOSThread()
}

func (c *Cocoa) bootstrapApp() error {
	app := objc.ID(objc.GetClass("NSApplication")).Send(selSharedApplication)
	if app == 0 {
		return errors.New("nsapplication unavailable")
	}
	app.Send(selSetActivationPolicy, nsApplicationActivationPolicyRegular)
	app.Send(selFinishLaunching)

	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc)
	pool = pool.Send(selInit)

	c.app = app
	c.pool = pool
	return nil
}

func (c *Cocoa) makeWindow(title string, width, height int) error {
	frame := NSRect{
		Origin: NSPoint{X: 100, Y: 100},
		Size:   NSSize{W: float64(width), H: float64(height)},
	}

	style := uint(nsWindowStyleTitled | nsWindowStyleClosable | nsWindowStyleMiniaturize | nsWindowStyleResizable)
	backing := uint(nsBackingStoreBuffered)

	winClass := objc.GetClass("NSWindow")
	win := objc.ID(winClass).Send(selAlloc)
	win = win.Send(selInitWithContentRect, frame, style, backing, false)
	if win == 0 {
		return errors.New("failed to create nswindow")
	}

	win.Send(selCenter)
	win.Send(selSetAcceptsMouseMoved, 1)
	win.Send(selSetReleasedWhenClosed, 0)
	titleStr := nsString(title)
	win.Send(selSetTitle, titleStr)
	win.Send(selMakeKeyAndOrderFront, objc.ID(0))

	c.window = win
	c.view = win.Send(selContentView)
	if c.view == 0 {
		return errors.New("window missing content view")
	}
	return nil
}

func (c *Cocoa) makeGLContext(useCoreProfile bool) error {
	attrs := []uint32{
		nsOpenGLPFAAccelerated,
		nsOpenGLPFADoubleBuffer,
		nsOpenGLPFAColorSize, 24,
		nsOpenGLPFADepthSize, 24,
		nsOpenGLPFAOpenGLProfile,
	}
	if useCoreProfile {
		attrs = append(attrs, nsOpenGLProfileVersion41Core)
	} else {
		attrs = append(attrs, nsOpenGLProfileVersionLegacy)
	}
	attrs = append(attrs, 0)

	pfClass := objc.GetClass("NSOpenGLPixelFormat")
	pf := objc.ID(pfClass).Send(selAlloc)
	pf = pf.Send(selInitWithAttributes, unsafe.Pointer(&attrs[0]))
	if pf == 0 {
		return errors.New("failed to create pixel format")
	}
	defer pf.Send(selRelease)

	ctxClass := objc.GetClass("NSOpenGLContext")
	ctx := objc.ID(ctxClass).Send(selAlloc)
	ctx = ctx.Send(selInitWithFormat, pf, objc.ID(0))
	if ctx == 0 {
		return errors.New("failed to create gl context")
	}

	ctx.Send(selSetView, c.view)
	ctx.Send(selMakeCurrentContext)

	// Enable vsync.
	swap := int32(1)
	ctx.Send(selSetValuesForParameter, unsafe.Pointer(&swap), nsOpenGLCPSwapInterval)

	c.ctx = ctx
	return nil
}

func ensureRuntime() error {
	initOnce.Do(func() {
		if err := loadObjc(); err != nil {
			initErr = err
			return
		}
		loadSelectors()
	})
	return initErr
}

func loadObjc() error {
	// Load libobjc and AppKit so the symbols are available.
	if _, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL); err != nil {
		return err
	}
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL); err != nil {
		return err
	}
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_GLOBAL)
	if err != nil {
		return err
	}

	purego.RegisterLibFunc(&cfRunLoopRunInMode, cf, "CFRunLoopRunInMode")
	ptr, err := purego.Dlsym(cf, "kCFRunLoopDefaultMode")
	if err != nil {
		return err
	}
	// Dlsym returns the address of the CFStringRef variable; read its value.
	cfDefaultMode = *(*uintptr)(unsafe.Pointer(ptr))

	return nil
}

func loadSelectors() {
	selAlloc = objc.RegisterName("alloc")
	selInit = objc.RegisterName("init")
	selRelease = objc.RegisterName("release")
	selSharedApplication = objc.RegisterName("sharedApplication")
	selNextEventMatchingMask = objc.RegisterName("nextEventMatchingMask:untilDate:inMode:dequeue:")
	selSetActivationPolicy = objc.RegisterName("setActivationPolicy:")
	selFinishLaunching = objc.RegisterName("finishLaunching")
	selStringWithUTF8String = objc.RegisterName("stringWithUTF8String:")
	selInitWithContentRect = objc.RegisterName("initWithContentRect:styleMask:backing:defer:")
	selMakeKeyAndOrderFront = objc.RegisterName("makeKeyAndOrderFront:")
	selSetTitle = objc.RegisterName("setTitle:")
	selSetAcceptsMouseMoved = objc.RegisterName("setAcceptsMouseMovedEvents:")
	selSetReleasedWhenClosed = objc.RegisterName("setReleasedWhenClosed:")
	selCenter = objc.RegisterName("center")
	selContentView = objc.RegisterName("contentView")
	selBounds = objc.RegisterName("bounds")
	selMouseLocationOutside = objc.RegisterName("mouseLocationOutsideOfEventStream")
	selConvertRectToBacking = objc.RegisterName("convertRectToBacking:")
	selIsVisible = objc.RegisterName("isVisible")
	selSendEvent = objc.RegisterName("sendEvent:")
	selFlushBuffer = objc.RegisterName("flushBuffer")
	selSetView = objc.RegisterName("setView:")
	selMakeCurrentContext = objc.RegisterName("makeCurrentContext")
	selClearCurrentContext = objc.RegisterName("clearCurrentContext")
	selInitWithAttributes = objc.RegisterName("initWithAttributes:")
	selInitWithFormat = objc.RegisterName("initWithFormat:shareContext:")
	selSetValuesForParameter = objc.RegisterName("setValues:forParameter:")
}

func nsString(v string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(selStringWithUTF8String, v+"\x00")
}

// cursorBackingPos returns the mouse in backing (pixel) coordinates.
func (c *Cocoa) cursorBackingPos() (float32, float32) {
	if c.window == 0 || c.view == 0 {
		return 0, 0
	}
	pos := objc.Send[NSPoint](c.window, selMouseLocationOutside)
	rect := NSRect{Origin: pos, Size: NSSize{W: 0, H: 0}}
	backing := objc.Send[NSRect](c.view, selConvertRectToBacking, rect)
	return float32(backing.Origin.X), float32(backing.Origin.Y)
}

func (c *Cocoa) Scale() float32 {
	// macOS handles scaling automatically through BackingSize()
	// which already accounts for Retina scaling, so we return 1.0
	// as the coordinate system is already scaled appropriately.
	return 1.0
}

func (c *Cocoa) GetKeyState(key Key) KeyState {
	// TODO: Implement key state tracking
	return KeyStateUp
}

func (c *Cocoa) GetButtonState(button Button) ButtonState {
	// TODO: Implement button state tracking
	return ButtonStateUp
}
