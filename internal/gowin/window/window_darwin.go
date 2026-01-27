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

	// Cached drawable metrics to detect resizes and keep the NSOpenGLContext
	// backing store in sync with the view size.
	lastBackingW int
	lastBackingH int

	// Input state tracking (frame-based).
	keyStates    map[Key]KeyState
	buttonStates map[Button]ButtonState

	// Text input buffered from keyDown events.
	textInput string

	// Raw input events queued during Poll().
	inputEvents []InputEvent

	// Dock menu support.
	dockMenu         objc.ID
	dockMenuItems    []DockMenuItem
	dockMenuCallback DockMenuCallback

	// URL handler for custom URL scheme events (e.g., crumblecracker://)
	urlHandler func(url string)
}

// Global URL queue for Apple Events callbacks (accessed from Objective-C runtime)
var (
	pendingURLs   []string
	pendingURLsMu sync.Mutex
)

var (
	initOnce sync.Once
	initErr  error

	// CoreFoundation.
	cfRunLoopRunInMode func(uintptr, float64, bool) int32
	cfDefaultMode      uintptr

	// Cached selectors.
	selAlloc                     objc.SEL
	selInit                      objc.SEL
	selRelease                   objc.SEL
	selSharedApplication         objc.SEL
	selNextEventMatchingMask     objc.SEL
	selSetActivationPolicy       objc.SEL
	selActivateIgnoringOtherApps objc.SEL
	selIsActive                  objc.SEL
	selFinishLaunching           objc.SEL
	selDistantPast               objc.SEL
	selStringWithUTF8String      objc.SEL
	selInitWithContentRect       objc.SEL
	selMakeKeyAndOrderFront      objc.SEL
	selSetTitle                  objc.SEL
	selSetAcceptsMouseMoved      objc.SEL
	selSetReleasedWhenClosed     objc.SEL
	selCenter                    objc.SEL
	selContentView               objc.SEL
	selBounds                    objc.SEL
	selMouseLocationOutside      objc.SEL
	selConvertRectToBacking      objc.SEL
	selIsVisible                 objc.SEL
	selSendEvent                 objc.SEL
	selFlushBuffer               objc.SEL
	selSetView                   objc.SEL
	selMakeCurrentContext        objc.SEL
	selClearCurrentContext       objc.SEL
	selUpdate                    objc.SEL
	selInitWithAttributes        objc.SEL
	selInitWithFormat            objc.SEL
	selSetValuesForParameter     objc.SEL

	// NSEvent selectors (input).
	selEventType       objc.SEL
	selEventKeyCode    objc.SEL
	selEventIsARepeat  objc.SEL
	selEventFlags      objc.SEL
	selEventButtonNum  objc.SEL
	selEventCharacters objc.SEL
	selEventDeltaY     objc.SEL
	selEventDeltaX     objc.SEL
	selEventScrollDY   objc.SEL
	selEventScrollDX   objc.SEL
	selUTF8String      objc.SEL

	// NSScreen selectors (display scale).
	selMainScreen         objc.SEL
	selBackingScaleFactor objc.SEL

	// NSMenu selectors (dock menu).
	selInitWithTitle  objc.SEL
	selAddItem        objc.SEL
	selRemoveAllItems objc.SEL
	selSeparatorItem  objc.SEL

	// NSMenuItem selectors.
	selInitWithTitleAction objc.SEL
	selSetTag              objc.SEL
	selGetTag              objc.SEL
	selSetEnabled          objc.SEL
	selSetTarget           objc.SEL

	// NSOpenPanel selectors (file dialogs).
	selOpenPanel               objc.SEL
	selSetCanChooseFiles       objc.SEL
	selSetCanChooseDirectories objc.SEL
	selSetAllowsMultiple       objc.SEL
	selSetAllowedFileTypes     objc.SEL
	selRunModal                objc.SEL
	selURLs                    objc.SEL
	selPath                    objc.SEL
	selCount                   objc.SEL
	selObjectAtIndex           objc.SEL

	// NSAppleEventManager selectors (URL handling).
	selSharedAppleEventManager objc.SEL
	selSetEventHandler         objc.SEL

	// NSAppleEventDescriptor selectors (URL extraction).
	selParamDescriptorForKeyword objc.SEL
	selStringValue               objc.SEL
)

// Subset of NSEventType values we care about.
// https://developer.apple.com/documentation/appkit/nsevent/eventtype
const (
	nsEventTypeLeftMouseDown  = 1
	nsEventTypeLeftMouseUp    = 2
	nsEventTypeRightMouseDown = 3
	nsEventTypeRightMouseUp   = 4
	nsEventTypeKeyDown        = 10
	nsEventTypeKeyUp          = 11
	nsEventTypeFlagsChanged   = 12
	nsEventTypeScrollWheel    = 22
	nsEventTypeOtherMouseDown = 25
	nsEventTypeOtherMouseUp   = 26
)

// Subset of NSEventModifierFlags values we care about.
// https://developer.apple.com/documentation/appkit/nseventmodifierflags
const (
	nsEventModifierFlagShift   = 1 << 17
	nsEventModifierFlagControl = 1 << 18
	nsEventModifierFlagOption  = 1 << 19
	nsEventModifierFlagCommand = 1 << 20
)

// Init boots Cocoa and OpenGL, keeping control of the run loop in Go.
func New(title string, width, height int, useCoreProfile bool) (Window, error) {
	runtime.LockOSThread()
	if err := ensureRuntime(); err != nil {
		return nil, err
	}

	c := &Cocoa{
		running:      true,
		keyStates:    make(map[Key]KeyState),
		buttonStates: make(map[Button]ButtonState),
		inputEvents:  make([]InputEvent, 0, 256),
	}
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

	// If the app isn't active, it won't receive key up / flagsChanged events.
	// That can leave modifiers "stuck" and make input feel broken when the
	// window comes up in the background. When inactive, clear transient input
	// state and rely on a fresh flagsChanged/keyDown once we regain focus.
	if c.app != 0 && objc.Send[bool](c.app, selIsActive) == false {
		for k := range c.keyStates {
			c.keyStates[k] = KeyStateUp
		}
		for b := range c.buttonStates {
			c.buttonStates[b] = ButtonStateUp
		}
		c.textInput = ""
	}

	// Transition states: Pressed -> Down, Released -> Up (once per Poll()).
	for key, state := range c.keyStates {
		if state == KeyStatePressed {
			c.keyStates[key] = KeyStateDown
		} else if state == KeyStateRepeated {
			// Repeated is a one-frame pulse indicating an OS key-repeat event.
			// If we don't transition it, downstream consumers may treat it as a
			// per-frame trigger and spam input.
			c.keyStates[key] = KeyStateDown
		} else if state == KeyStateReleased {
			c.keyStates[key] = KeyStateUp
		}
	}
	for button, state := range c.buttonStates {
		if state == ButtonStatePressed {
			c.buttonStates[button] = ButtonStateDown
		} else if state == ButtonStateReleased {
			c.buttonStates[button] = ButtonStateUp
		}
	}

	// Drain one slice of the run loop without blocking and pump pending NSEvents.
	cfRunLoopRunInMode(cfDefaultMode, 0, true)
	for {
		// Use NSDate.distantPast to ensure this call is non-blocking. Passing nil
		// can block waiting for an event, which will stall the render loop and
		// make the window appear hung/blank.
		untilDate := objc.ID(objc.GetClass("NSDate")).Send(selDistantPast)
		ev := objc.Send[objc.ID](c.app, selNextEventMatchingMask, nsEventMaskAny, untilDate, objc.ID(cfDefaultMode), true)
		if ev == 0 {
			break
		}

		etype := int64(objc.Send[uint64](ev, selEventType))
		c.processEvent(ev)

		// We consume keyboard input ourselves. Forwarding key events into the
		// normal Cocoa responder chain (with no first responder text view) causes
		// the system beep on every key press.
		switch etype {
		case nsEventTypeKeyDown, nsEventTypeKeyUp, nsEventTypeFlagsChanged:
			// Do not forward.
		default:
			c.app.Send(selSendEvent, ev)
		}
	}

	if !objc.Send[bool](c.window, selIsVisible) {
		c.running = false
	}

	// Keep the OpenGL drawable sized correctly across resizes/maximize/fullscreen.
	c.updateDrawableIfNeeded()

	// Process any pending URL events from Apple Events
	c.drainPendingURLs()

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
	// When launched from a terminal (no .app bundle), Cocoa often won't make
	// the process frontmost automatically. Without activation, the window may
	// appear behind other apps and keyboard input can behave inconsistently.
	app.Send(selActivateIgnoringOtherApps, true)

	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc)
	pool = pool.Send(selInit)

	// Set the default run loop mode. This is toll-free bridged between CFString
	// and NSString, and avoids unsafe symbol dereferencing.
	if cfDefaultMode == 0 {
		cfDefaultMode = uintptr(nsString("kCFRunLoopDefaultMode"))
	}

	c.app = app
	c.pool = pool

	// Register for URL open events (Apple Events)
	c.registerURLEventHandler()

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
	// Best-effort: ensure the window becomes frontmost + key.
	if c.app != 0 {
		c.app.Send(selActivateIgnoringOtherApps, true)
	}

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

	return nil
}

func loadSelectors() {
	selAlloc = objc.RegisterName("alloc")
	selInit = objc.RegisterName("init")
	selRelease = objc.RegisterName("release")
	selSharedApplication = objc.RegisterName("sharedApplication")
	selNextEventMatchingMask = objc.RegisterName("nextEventMatchingMask:untilDate:inMode:dequeue:")
	selSetActivationPolicy = objc.RegisterName("setActivationPolicy:")
	selActivateIgnoringOtherApps = objc.RegisterName("activateIgnoringOtherApps:")
	selIsActive = objc.RegisterName("isActive")
	selFinishLaunching = objc.RegisterName("finishLaunching")
	selDistantPast = objc.RegisterName("distantPast")
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
	selUpdate = objc.RegisterName("update")
	selInitWithAttributes = objc.RegisterName("initWithAttributes:")
	selInitWithFormat = objc.RegisterName("initWithFormat:shareContext:")
	selSetValuesForParameter = objc.RegisterName("setValues:forParameter:")

	// NSEvent (input).
	selEventType = objc.RegisterName("type")
	selEventKeyCode = objc.RegisterName("keyCode")
	selEventIsARepeat = objc.RegisterName("isARepeat")
	selEventFlags = objc.RegisterName("modifierFlags")
	selEventButtonNum = objc.RegisterName("buttonNumber")
	selEventCharacters = objc.RegisterName("characters")
	selEventDeltaY = objc.RegisterName("deltaY")
	selEventDeltaX = objc.RegisterName("deltaX")
	selEventScrollDY = objc.RegisterName("scrollingDeltaY")
	selEventScrollDX = objc.RegisterName("scrollingDeltaX")
	selUTF8String = objc.RegisterName("UTF8String")

	// NSScreen (display scale).
	selMainScreen = objc.RegisterName("mainScreen")
	selBackingScaleFactor = objc.RegisterName("backingScaleFactor")

	// NSMenu (dock menu).
	selInitWithTitle = objc.RegisterName("initWithTitle:")
	selAddItem = objc.RegisterName("addItem:")
	selRemoveAllItems = objc.RegisterName("removeAllItems")
	selSeparatorItem = objc.RegisterName("separatorItem")

	// NSMenuItem.
	selInitWithTitleAction = objc.RegisterName("initWithTitle:action:keyEquivalent:")
	selSetTag = objc.RegisterName("setTag:")
	selGetTag = objc.RegisterName("tag")
	selSetEnabled = objc.RegisterName("setEnabled:")
	selSetTarget = objc.RegisterName("setTarget:")

	// NSOpenPanel (file dialogs).
	selOpenPanel = objc.RegisterName("openPanel")
	selSetCanChooseFiles = objc.RegisterName("setCanChooseFiles:")
	selSetCanChooseDirectories = objc.RegisterName("setCanChooseDirectories:")
	selSetAllowsMultiple = objc.RegisterName("setAllowsMultipleSelection:")
	selSetAllowedFileTypes = objc.RegisterName("setAllowedFileTypes:")
	selRunModal = objc.RegisterName("runModal")
	selURLs = objc.RegisterName("URLs")
	selPath = objc.RegisterName("path")
	selCount = objc.RegisterName("count")
	selObjectAtIndex = objc.RegisterName("objectAtIndex:")

	// NSAppleEventManager (URL handling).
	selSharedAppleEventManager = objc.RegisterName("sharedAppleEventManager")
	selSetEventHandler = objc.RegisterName("setEventHandler:andSelector:forEventClass:andEventID:")

	// NSAppleEventDescriptor (URL extraction).
	selParamDescriptorForKeyword = objc.RegisterName("paramDescriptorForKeyword:")
	selStringValue = objc.RegisterName("stringValue")
}

func nsString(v string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(selStringWithUTF8String, v+"\x00")
}

func nsStringToGo(v objc.ID) string {
	if v == 0 {
		return ""
	}
	ptr := objc.Send[unsafe.Pointer](v, selUTF8String)
	if ptr == nil {
		return ""
	}
	// Find NUL terminator.
	n := 0
	for {
		if *(*byte)(unsafe.Add(ptr, n)) == 0 {
			break
		}
		n++
	}
	if n == 0 {
		return ""
	}
	// Make a copy of the string data since the NSString buffer may be released.
	// unsafe.String just points to the original memory which can become invalid.
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = *(*byte)(unsafe.Add(ptr, i))
	}
	return string(b)
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
	// Return the backing (pixel) to logical (point) scale factor. This is
	// typically 2.0 on Retina displays.
	if c.view == 0 {
		return 1.0
	}

	bounds := objc.Send[NSRect](c.view, selBounds)
	if bounds.Size.W == 0 || bounds.Size.H == 0 {
		return 1.0
	}
	backing := objc.Send[NSRect](c.view, selConvertRectToBacking, bounds)

	sx := float32(backing.Size.W / bounds.Size.W)
	sy := float32(backing.Size.H / bounds.Size.H)
	if sx <= 0 {
		sx = 1.0
	}
	if sy <= 0 {
		sy = 1.0
	}
	if sx > sy {
		return sx
	}
	return sy
}

func (c *Cocoa) updateDrawableIfNeeded() {
	if c.ctx == 0 || c.view == 0 || selUpdate == 0 {
		return
	}

	// Detect backing size changes and call -[NSOpenGLContext update] to ensure
	// the drawable matches the view. Without this, macOS can keep the old
	// backing store size after window zoom/maximize, causing incorrect scaling.
	bw, bh := c.BackingSize()
	if bw <= 0 || bh <= 0 {
		return
	}
	if bw == c.lastBackingW && bh == c.lastBackingH {
		return
	}
	c.lastBackingW, c.lastBackingH = bw, bh

	// Ensure we're updating the correct context.
	c.ctx.Send(selMakeCurrentContext)
	c.ctx.Send(selUpdate)
}

func (c *Cocoa) GetKeyState(key Key) KeyState {
	if c.keyStates == nil {
		return KeyStateUp
	}
	if state, ok := c.keyStates[key]; ok {
		return state
	}
	return KeyStateUp
}

func (c *Cocoa) GetButtonState(button Button) ButtonState {
	if c.buttonStates == nil {
		return ButtonStateUp
	}
	if state, ok := c.buttonStates[button]; ok {
		return state
	}
	return ButtonStateUp
}

func (c *Cocoa) DrainInputEvents() []InputEvent {
	if c == nil || len(c.inputEvents) == 0 {
		return nil
	}
	out := make([]InputEvent, len(c.inputEvents))
	copy(out, c.inputEvents)
	c.inputEvents = c.inputEvents[:0]
	return out
}

func (c *Cocoa) TextInput() string {
	s := c.textInput
	c.textInput = ""
	return s
}

func cocoaFlagsToMods(flags uint64) KeyMods {
	var m KeyMods
	if (flags & nsEventModifierFlagShift) != 0 {
		m |= ModShift
	}
	if (flags & nsEventModifierFlagControl) != 0 {
		m |= ModCtrl
	}
	if (flags & nsEventModifierFlagOption) != 0 {
		m |= ModAlt
	}
	if (flags & nsEventModifierFlagCommand) != 0 {
		m |= ModSuper
	}
	return m
}

func cocoaKeyEmitsText(key Key) bool {
	switch key {
	case KeyUnknown,
		KeyEnter, KeyTab, KeyBackspace, KeyEscape, KeyDelete,
		KeyUp, KeyDown, KeyLeft, KeyRight,
		KeyHome, KeyEnd, KeyPageUp, KeyPageDown, KeyInsert,
		KeyCapsLock, KeyScrollLock, KeyNumLock,
		KeyPrintScreen, KeyPause,
		KeyF1, KeyF2, KeyF3, KeyF4, KeyF5, KeyF6, KeyF7, KeyF8, KeyF9, KeyF10, KeyF11, KeyF12:
		return false
	default:
		return true
	}
}

func (c *Cocoa) processEvent(ev objc.ID) {
	etype := int64(objc.Send[uint64](ev, selEventType))
	switch etype {
	case nsEventTypeKeyDown:
		keyCode := uint16(objc.Send[uint64](ev, selEventKeyCode))
		key := cocoaKeyCodeToKey(keyCode)
		if key == KeyUnknown {
			return
		}
		flags := objc.Send[uint64](ev, selEventFlags)
		mods := cocoaFlagsToMods(flags)
		chars := objc.Send[objc.ID](ev, selEventCharacters)
		if s := nsStringToGo(chars); s != "" {
			c.textInput += s
			// Don't emit TextEvent for keyboard shortcuts (Cmd+key or Ctrl+key)
			isShortcut := (mods & (ModSuper | ModCtrl)) != 0
			if cocoaKeyEmitsText(key) && !isShortcut {
				c.inputEvents = append(c.inputEvents, InputEvent{
					Type: InputEventText,
					Text: s,
					Mods: mods,
				})
			}
		}
		isRepeat := objc.Send[bool](ev, selEventIsARepeat)

		c.inputEvents = append(c.inputEvents, InputEvent{
			Type:   InputEventKeyDown,
			Key:    key,
			Repeat: isRepeat,
			Mods:   mods,
		})

		prev := c.GetKeyState(key)
		if isRepeat || prev.IsDown() {
			c.keyStates[key] = KeyStateRepeated
		} else {
			c.keyStates[key] = KeyStatePressed
		}

	case nsEventTypeKeyUp:
		keyCode := uint16(objc.Send[uint64](ev, selEventKeyCode))
		key := cocoaKeyCodeToKey(keyCode)
		if key == KeyUnknown {
			return
		}
		flags := objc.Send[uint64](ev, selEventFlags)
		c.inputEvents = append(c.inputEvents, InputEvent{
			Type: InputEventKeyUp,
			Key:  key,
			Mods: cocoaFlagsToMods(flags),
		})
		c.keyStates[key] = KeyStateReleased

	case nsEventTypeFlagsChanged:
		// Modifiers typically come through as flagsChanged rather than keyDown/keyUp.
		keyCode := uint16(objc.Send[uint64](ev, selEventKeyCode))
		key := cocoaKeyCodeToKey(keyCode)
		if key == KeyUnknown {
			return
		}
		flags := objc.Send[uint64](ev, selEventFlags)
		c.inputEvents = append(c.inputEvents, InputEvent{
			Type: InputEventFlagsChanged,
			Key:  key,
			Mods: cocoaFlagsToMods(flags),
		})
		isDown := cocoaModifierKeyIsDown(key, flags)
		c.setKeyDown(key, isDown)

	case nsEventTypeLeftMouseDown, nsEventTypeRightMouseDown, nsEventTypeOtherMouseDown:
		buttonNum := int64(objc.Send[uint64](ev, selEventButtonNum))
		if button, ok := cocoaButtonNumberToButton(buttonNum); ok {
			c.buttonStates[button] = ButtonStatePressed
			flags := objc.Send[uint64](ev, selEventFlags)
			c.inputEvents = append(c.inputEvents, InputEvent{
				Type:   InputEventMouseDown,
				Button: button,
				Mods:   cocoaFlagsToMods(flags),
			})
		}

	case nsEventTypeLeftMouseUp, nsEventTypeRightMouseUp, nsEventTypeOtherMouseUp:
		buttonNum := int64(objc.Send[uint64](ev, selEventButtonNum))
		if button, ok := cocoaButtonNumberToButton(buttonNum); ok {
			c.buttonStates[button] = ButtonStateReleased
			flags := objc.Send[uint64](ev, selEventFlags)
			c.inputEvents = append(c.inputEvents, InputEvent{
				Type:   InputEventMouseUp,
				Button: button,
				Mods:   cocoaFlagsToMods(flags),
			})
		}

	case nsEventTypeScrollWheel:
		flags := objc.Send[uint64](ev, selEventFlags)
		// Prefer "scrollingDeltaY/X" (trackpad + mouse), fall back to "deltaY/X".
		dy := objc.Send[float64](ev, selEventScrollDY)
		dx := objc.Send[float64](ev, selEventScrollDX)
		if dy == 0 {
			dy = objc.Send[float64](ev, selEventDeltaY)
		}
		if dx == 0 {
			dx = objc.Send[float64](ev, selEventDeltaX)
		}
		if dy != 0 || dx != 0 {
			// Convert to rough "wheel ticks". For precise devices, deltas can be small.
			const tick = 10.0
			c.inputEvents = append(c.inputEvents, InputEvent{
				Type:    InputEventScroll,
				ScrollX: float32(dx / tick),
				ScrollY: float32(dy / tick),
				Mods:    cocoaFlagsToMods(flags),
			})
		}
	}
}

func (c *Cocoa) setKeyDown(key Key, down bool) {
	prev := c.GetKeyState(key)
	if down {
		if prev == KeyStateUp || prev == KeyStateReleased {
			c.keyStates[key] = KeyStatePressed
		} else {
			c.keyStates[key] = KeyStateDown
		}
		return
	}

	if prev.IsDown() {
		c.keyStates[key] = KeyStateReleased
	} else {
		c.keyStates[key] = KeyStateUp
	}
}

func cocoaButtonNumberToButton(n int64) (Button, bool) {
	switch n {
	case 0:
		return ButtonLeft, true
	case 1:
		return ButtonRight, true
	case 2:
		return ButtonMiddle, true
	case 3:
		return Button4, true
	case 4:
		return Button5, true
	default:
		return ButtonLeft, false
	}
}

func cocoaModifierKeyIsDown(key Key, flags uint64) bool {
	switch key {
	case KeyLeftShift, KeyRightShift:
		return (flags & nsEventModifierFlagShift) != 0
	case KeyLeftControl, KeyRightControl:
		return (flags & nsEventModifierFlagControl) != 0
	case KeyLeftAlt, KeyRightAlt:
		return (flags & nsEventModifierFlagOption) != 0
	case KeyLeftSuper, KeyRightSuper:
		return (flags & nsEventModifierFlagCommand) != 0
	default:
		return false
	}
}

// cocoaKeyCodeToKey maps macOS virtual keycodes (hardware-dependent, but stable on Apple keyboards)
// to our cross-platform Key enum.
//
// Keycode reference (commonly cited):
// https://developer.apple.com/library/archive/technotes/tn2450/_index.html
func cocoaKeyCodeToKey(code uint16) Key {
	switch code {
	// Letters.
	case 0:
		return KeyA
	case 1:
		return KeyS
	case 2:
		return KeyD
	case 3:
		return KeyF
	case 4:
		return KeyH
	case 5:
		return KeyG
	case 6:
		return KeyZ
	case 7:
		return KeyX
	case 8:
		return KeyC
	case 9:
		return KeyV
	case 11:
		return KeyB
	case 12:
		return KeyQ
	case 13:
		return KeyW
	case 14:
		return KeyE
	case 15:
		return KeyR
	case 16:
		return KeyY
	case 17:
		return KeyT
	case 31:
		return KeyO
	case 32:
		return KeyU
	case 34:
		return KeyI
	case 35:
		return KeyP
	case 37:
		return KeyL
	case 38:
		return KeyJ
	case 40:
		return KeyK
	case 45:
		return KeyN
	case 46:
		return KeyM

	// Numbers (top row).
	case 18:
		return Key1
	case 19:
		return Key2
	case 20:
		return Key3
	case 21:
		return Key4
	case 23:
		return Key5
	case 22:
		return Key6
	case 26:
		return Key7
	case 28:
		return Key8
	case 25:
		return Key9
	case 29:
		return Key0

	// Function keys.
	case 122:
		return KeyF1
	case 120:
		return KeyF2
	case 99:
		return KeyF3
	case 118:
		return KeyF4
	case 96:
		return KeyF5
	case 97:
		return KeyF6
	case 98:
		return KeyF7
	case 100:
		return KeyF8
	case 101:
		return KeyF9
	case 109:
		return KeyF10
	case 103:
		return KeyF11
	case 111:
		return KeyF12

	// Modifiers.
	case 56:
		return KeyLeftShift
	case 60:
		return KeyRightShift
	case 59:
		return KeyLeftControl
	case 62:
		return KeyRightControl
	case 58:
		return KeyLeftAlt
	case 61:
		return KeyRightAlt
	case 55:
		return KeyLeftSuper
	case 54:
		return KeyRightSuper

	// Special keys.
	case 49:
		return KeySpace
	case 36:
		return KeyEnter
	case 53:
		return KeyEscape
	case 51:
		return KeyBackspace
	case 117:
		return KeyDelete
	case 48:
		return KeyTab
	case 57:
		return KeyCapsLock

	// Arrow keys.
	case 126:
		return KeyUp
	case 125:
		return KeyDown
	case 123:
		return KeyLeft
	case 124:
		return KeyRight

	// Navigation keys.
	case 115:
		return KeyHome
	case 119:
		return KeyEnd
	case 116:
		return KeyPageUp
	case 121:
		return KeyPageDown
	case 114: // Help key (often mapped as Insert on extended keyboards)
		return KeyInsert

	// Punctuation and symbols.
	case 50:
		return KeyGraveAccent
	case 27:
		return KeyMinus
	case 24:
		return KeyEqual
	case 33:
		return KeyLeftBracket
	case 30:
		return KeyRightBracket
	case 42:
		return KeyBackslash
	case 41:
		return KeySemicolon
	case 39:
		return KeyApostrophe
	case 43:
		return KeyComma
	case 47:
		return KeyPeriod
	case 44:
		return KeySlash

	// Numpad keys.
	case 82:
		return KeyNumpad0
	case 83:
		return KeyNumpad1
	case 84:
		return KeyNumpad2
	case 85:
		return KeyNumpad3
	case 86:
		return KeyNumpad4
	case 87:
		return KeyNumpad5
	case 88:
		return KeyNumpad6
	case 89:
		return KeyNumpad7
	case 91:
		return KeyNumpad8
	case 92:
		return KeyNumpad9
	case 65:
		return KeyNumpadDecimal
	case 75:
		return KeyNumpadDivide
	case 67:
		return KeyNumpadMultiply
	case 78:
		return KeyNumpadSubtract
	case 69:
		return KeyNumpadAdd
	case 76:
		return KeyNumpadEnter
	case 81:
		return KeyNumpadEqual
	}
	return KeyUnknown
}

// getDisplayScale returns the display scale factor.
// On macOS, this is typically 2.0 on Retina displays. This is used before
// creating a window to pick a sensible physical size for a desired logical size.
func getDisplayScale() float32 {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := ensureRuntime(); err != nil {
		return 1.0
	}

	// Create a small autorelease pool for this query since it can be called
	// before we bootstrap the NSApplication/pool.
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc)
	pool = pool.Send(selInit)
	if pool != 0 {
		defer pool.Send(selRelease)
	}

	screenClass := objc.GetClass("NSScreen")
	if screenClass == 0 {
		return 1.0
	}
	main := objc.ID(screenClass).Send(selMainScreen)
	if main == 0 {
		return 1.0
	}

	scale := float32(objc.Send[float64](main, selBackingScaleFactor))
	if scale <= 0 {
		return 1.0
	}
	return scale
}

// SetDockMenu implements DockMenuSupport interface.
// It sets up the dock menu with the given items and callback.
// Note: macOS has one dock menu per application, not per window.
// The last window to call SetDockMenu owns the dock menu.
func (c *Cocoa) SetDockMenu(items []DockMenuItem, callback DockMenuCallback) {
	c.dockMenuItems = items
	c.dockMenuCallback = callback

	// Create or update the dock menu
	c.rebuildDockMenu()
}

func (c *Cocoa) rebuildDockMenu() {
	if c.dockMenu == 0 {
		// Create the dock menu
		menu := objc.ID(objc.GetClass("NSMenu")).Send(selAlloc)
		menu = menu.Send(selInitWithTitle, nsString(""))
		c.dockMenu = menu
	} else {
		// Clear existing items
		c.dockMenu.Send(selRemoveAllItems)
	}

	// Add items
	for _, item := range c.dockMenuItems {
		if item.Separator {
			sep := objc.ID(objc.GetClass("NSMenuItem")).Send(selSeparatorItem)
			c.dockMenu.Send(selAddItem, sep)
			continue
		}

		// Create menu item with no action - we'll poll for clicks
		menuItem := objc.ID(objc.GetClass("NSMenuItem")).Send(selAlloc)
		menuItem = menuItem.Send(selInitWithTitleAction, nsString(item.Title), objc.SEL(0), nsString(""))
		menuItem.Send(selSetTag, int64(item.Tag))
		menuItem.Send(selSetEnabled, item.Enabled)
		c.dockMenu.Send(selAddItem, menuItem)
	}
}

// GetDockMenu returns the NSMenu for the dock. This should be called from
// the application delegate's applicationDockMenu: method.
func (c *Cocoa) GetDockMenu() objc.ID {
	return c.dockMenu
}

// HandleDockMenuAction is called when a dock menu item is clicked.
// The tag identifies which item was clicked.
func (c *Cocoa) HandleDockMenuAction(tag int) {
	if c.dockMenuCallback != nil {
		c.dockMenuCallback(tag)
	}
}

// ShowOpenPanel implements FileDialogSupport interface.
// It shows a native file/directory open dialog and returns the selected path.
func (c *Cocoa) ShowOpenPanel(dialogType FileDialogType, allowedExtensions []string) string {
	// NSOpenPanel must be shown on the main thread
	panel := objc.ID(objc.GetClass("NSOpenPanel")).Send(selOpenPanel)
	if panel == 0 {
		return ""
	}

	switch dialogType {
	case FileDialogTypeDirectory:
		panel.Send(selSetCanChooseFiles, false)
		panel.Send(selSetCanChooseDirectories, true)
	case FileDialogTypeFile:
		panel.Send(selSetCanChooseFiles, true)
		panel.Send(selSetCanChooseDirectories, false)
		if len(allowedExtensions) > 0 {
			// Build NSArray of allowed file types
			arrayClass := objc.GetClass("NSMutableArray")
			arr := objc.ID(arrayClass).Send(selAlloc)
			arr = arr.Send(selInit)
			selAddObject := objc.RegisterName("addObject:")
			for _, ext := range allowedExtensions {
				arr.Send(selAddObject, nsString(ext))
			}
			panel.Send(selSetAllowedFileTypes, arr)
		}
	}

	panel.Send(selSetAllowsMultiple, false)

	// NSModalResponseOK = 1
	result := objc.Send[int64](panel, selRunModal)
	if result != 1 {
		return ""
	}

	urls := panel.Send(selURLs)
	if urls == 0 {
		return ""
	}

	count := objc.Send[uint64](urls, selCount)
	if count == 0 {
		return ""
	}

	firstURL := objc.Send[objc.ID](urls, selObjectAtIndex, uint64(0))
	if firstURL == 0 {
		return ""
	}

	pathStr := firstURL.Send(selPath)
	return nsStringToGo(pathStr)
}

// SetURLHandler implements URLEventSupport interface.
// It sets a callback to receive URLs opened via custom URL schemes.
func (c *Cocoa) SetURLHandler(handler func(url string)) {
	c.urlHandler = handler
}

// drainPendingURLs processes any URLs queued from Apple Events.
func (c *Cocoa) drainPendingURLs() {
	pendingURLsMu.Lock()
	urls := pendingURLs
	pendingURLs = nil
	pendingURLsMu.Unlock()

	for _, url := range urls {
		if c.urlHandler != nil {
			c.urlHandler(url)
		}
	}
}

// queuePendingURL adds a URL to the pending queue (called from Apple Event handler).
func queuePendingURL(url string) {
	pendingURLsMu.Lock()
	pendingURLs = append(pendingURLs, url)
	pendingURLsMu.Unlock()
}

// Apple Event constants for URL handling.
const (
	// kInternetEventClass = 'GURL' = 0x4755524C
	kInternetEventClass uint32 = 0x4755524C
	// kAEGetURL = 'GURL' = 0x4755524C
	kAEGetURL uint32 = 0x4755524C
	// keyDirectObject = '----' = 0x2D2D2D2D
	keyDirectObject uint32 = 0x2D2D2D2D
)

// URL handler class name
const urlHandlerClassName = "CCURLHandler"

// Global reference to the URL handler class (created once)
var urlHandlerClass objc.Class

// Global selector for the URL handler method
var selHandleGetURL objc.SEL

// registerURLEventHandler sets up Apple Event handling for URLs.
// This creates a custom Objective-C class to receive kAEGetURL events.
func (c *Cocoa) registerURLEventHandler() {
	// Only register once
	if urlHandlerClass != 0 {
		return
	}

	// Register the selector for our handler method
	selHandleGetURL = objc.RegisterName("handleGetURL:withReplyEvent:")

	// Create the handler class using objc.RegisterClass
	// This is a minimal class that can receive Apple Events
	var regErr error
	urlHandlerClass, regErr = objc.RegisterClass(
		urlHandlerClassName,
		objc.GetClass("NSObject"),
		nil, // No ivars
		nil, // No protocols
		[]objc.MethodDef{
			{
				Cmd: selHandleGetURL,
				Fn:  handleGetURLEvent,
			},
		},
	)

	if urlHandlerClass == 0 || regErr != nil {
		if regErr != nil {
			println("window_darwin: failed to register URL handler class:", regErr.Error())
		} else {
			println("window_darwin: failed to register URL handler class: unknown error")
		}
		return
	}
	println("window_darwin: registered URL handler class")

	// Create an instance of our handler class
	handler := objc.ID(urlHandlerClass).Send(selAlloc)
	handler = handler.Send(selInit)
	if handler == 0 {
		println("window_darwin: failed to create URL handler instance")
		return
	}
	println("window_darwin: created URL handler instance")

	// Get the shared Apple Event manager
	aem := objc.ID(objc.GetClass("NSAppleEventManager")).Send(selSharedAppleEventManager)
	if aem == 0 {
		println("window_darwin: failed to get NSAppleEventManager")
		return
	}

	// Register for kAEGetURL events
	// setEventHandler:andSelector:forEventClass:andEventID:
	aem.Send(selSetEventHandler,
		handler,
		selHandleGetURL,
		kInternetEventClass,
		kAEGetURL,
	)
	println("window_darwin: registered for kAEGetURL events")
}

// handleGetURLEvent is the Objective-C method called when a URL event is received.
// Signature: - (void)handleGetURL:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)replyEvent
// Note: purego requires _cmd (selector) as the second parameter after self
func handleGetURLEvent(self objc.ID, _cmd objc.SEL, event objc.ID, replyEvent objc.ID) {
	println("window_darwin: handleGetURLEvent called!")
	if event == 0 {
		println("window_darwin: event is nil")
		return
	}

	// Get the direct parameter which contains the URL
	// paramDescriptorForKeyword: takes a keyword (uint32)
	urlDesc := objc.Send[objc.ID](event, selParamDescriptorForKeyword, keyDirectObject)
	if urlDesc == 0 {
		println("window_darwin: urlDesc is nil")
		return
	}

	// Get the string value of the URL
	urlNSString := urlDesc.Send(selStringValue)
	if urlNSString == 0 {
		println("window_darwin: urlNSString is nil")
		return
	}

	// Convert to Go string and queue it
	url := nsStringToGo(urlNSString)
	println("window_darwin: received URL:", url)
	if url != "" {
		queuePendingURL(url)
	}
}
