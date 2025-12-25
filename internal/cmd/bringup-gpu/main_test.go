//go:build guest

// Package main provides a GPU bringup test that runs inside the guest VM.
// It tests the virtio-gpu framebuffer and virtio-input devices by displaying
// a cursor and pressed keys on the screen.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Framebuffer device constants
const (
	fbDevice    = "/dev/fb0"
	inputDevice = "/dev/input/event0"
)

// Linux framebuffer IOCTL commands
const (
	FBIOGET_VSCREENINFO = 0x4600
	FBIOPUT_VSCREENINFO = 0x4601
	FBIOGET_FSCREENINFO = 0x4602
)

// fb_var_screeninfo structure (simplified - key fields only)
type fbVarScreenInfo struct {
	XRes         uint32
	YRes         uint32
	XResVirtual  uint32
	YResVirtual  uint32
	XOffset      uint32
	YOffset      uint32
	BitsPerPixel uint32
	Grayscale    uint32
	Red          fbBitfield
	Green        fbBitfield
	Blue         fbBitfield
	Transp       fbBitfield
	Nonstd       uint32
	Activate     uint32
	Height       uint32
	Width        uint32
	AccelFlags   uint32
	// Timing info (we don't care about these for virtio-gpu)
	Pixclock    uint32
	LeftMargin  uint32
	RightMargin uint32
	UpperMargin uint32
	LowerMargin uint32
	HSyncLen    uint32
	VSyncLen    uint32
	Sync        uint32
	Vmode       uint32
	Rotate      uint32
	Colorspace  uint32
	Reserved    [4]uint32
}

type fbBitfield struct {
	Offset uint32
	Length uint32
	Msb    uint32
}

// fb_fix_screeninfo structure (simplified)
type fbFixScreenInfo struct {
	ID           [16]byte
	SmemStart    uint64
	SmemLen      uint32
	Type         uint32
	TypeAux      uint32
	Visual       uint32
	XPanStep     uint16
	YPanStep     uint16
	YWrapStep    uint16
	_            uint16
	LineLength   uint32
	MmioStart    uint64
	MmioLen      uint32
	Accel        uint32
	Capabilities uint16
	Reserved     [2]uint16
}

// Input event structure (struct input_event)
type inputEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

// Linux input event types
const (
	EV_SYN = 0x00
	EV_KEY = 0x01
	EV_ABS = 0x03
)

// Absolute axis codes
const (
	ABS_X = 0x00
	ABS_Y = 0x01
)

// Simple font for rendering text (8x8 bitmap font for ASCII)
// Each byte represents one row of the character
var font8x8 = map[byte][8]byte{
	' ':  {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	'A':  {0x18, 0x3C, 0x66, 0x7E, 0x66, 0x66, 0x66, 0x00},
	'B':  {0x7C, 0x66, 0x66, 0x7C, 0x66, 0x66, 0x7C, 0x00},
	'C':  {0x3C, 0x66, 0x60, 0x60, 0x60, 0x66, 0x3C, 0x00},
	'D':  {0x78, 0x6C, 0x66, 0x66, 0x66, 0x6C, 0x78, 0x00},
	'E':  {0x7E, 0x60, 0x60, 0x7C, 0x60, 0x60, 0x7E, 0x00},
	'F':  {0x7E, 0x60, 0x60, 0x7C, 0x60, 0x60, 0x60, 0x00},
	'G':  {0x3C, 0x66, 0x60, 0x6E, 0x66, 0x66, 0x3C, 0x00},
	'H':  {0x66, 0x66, 0x66, 0x7E, 0x66, 0x66, 0x66, 0x00},
	'I':  {0x3C, 0x18, 0x18, 0x18, 0x18, 0x18, 0x3C, 0x00},
	'J':  {0x1E, 0x0C, 0x0C, 0x0C, 0x0C, 0x6C, 0x38, 0x00},
	'K':  {0x66, 0x6C, 0x78, 0x70, 0x78, 0x6C, 0x66, 0x00},
	'L':  {0x60, 0x60, 0x60, 0x60, 0x60, 0x60, 0x7E, 0x00},
	'M':  {0x63, 0x77, 0x7F, 0x6B, 0x63, 0x63, 0x63, 0x00},
	'N':  {0x66, 0x76, 0x7E, 0x7E, 0x6E, 0x66, 0x66, 0x00},
	'O':  {0x3C, 0x66, 0x66, 0x66, 0x66, 0x66, 0x3C, 0x00},
	'P':  {0x7C, 0x66, 0x66, 0x7C, 0x60, 0x60, 0x60, 0x00},
	'Q':  {0x3C, 0x66, 0x66, 0x66, 0x66, 0x3C, 0x0E, 0x00},
	'R':  {0x7C, 0x66, 0x66, 0x7C, 0x78, 0x6C, 0x66, 0x00},
	'S':  {0x3C, 0x66, 0x60, 0x3C, 0x06, 0x66, 0x3C, 0x00},
	'T':  {0x7E, 0x18, 0x18, 0x18, 0x18, 0x18, 0x18, 0x00},
	'U':  {0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x3C, 0x00},
	'V':  {0x66, 0x66, 0x66, 0x66, 0x66, 0x3C, 0x18, 0x00},
	'W':  {0x63, 0x63, 0x63, 0x6B, 0x7F, 0x77, 0x63, 0x00},
	'X':  {0x66, 0x66, 0x3C, 0x18, 0x3C, 0x66, 0x66, 0x00},
	'Y':  {0x66, 0x66, 0x66, 0x3C, 0x18, 0x18, 0x18, 0x00},
	'Z':  {0x7E, 0x06, 0x0C, 0x18, 0x30, 0x60, 0x7E, 0x00},
	'0':  {0x3C, 0x66, 0x6E, 0x76, 0x66, 0x66, 0x3C, 0x00},
	'1':  {0x18, 0x18, 0x38, 0x18, 0x18, 0x18, 0x7E, 0x00},
	'2':  {0x3C, 0x66, 0x06, 0x0C, 0x30, 0x60, 0x7E, 0x00},
	'3':  {0x3C, 0x66, 0x06, 0x1C, 0x06, 0x66, 0x3C, 0x00},
	'4':  {0x06, 0x0E, 0x1E, 0x66, 0x7F, 0x06, 0x06, 0x00},
	'5':  {0x7E, 0x60, 0x7C, 0x06, 0x06, 0x66, 0x3C, 0x00},
	'6':  {0x3C, 0x66, 0x60, 0x7C, 0x66, 0x66, 0x3C, 0x00},
	'7':  {0x7E, 0x66, 0x0C, 0x18, 0x18, 0x18, 0x18, 0x00},
	'8':  {0x3C, 0x66, 0x66, 0x3C, 0x66, 0x66, 0x3C, 0x00},
	'9':  {0x3C, 0x66, 0x66, 0x3E, 0x06, 0x66, 0x3C, 0x00},
	':':  {0x00, 0x00, 0x18, 0x00, 0x00, 0x18, 0x00, 0x00},
	'-':  {0x00, 0x00, 0x00, 0x7E, 0x00, 0x00, 0x00, 0x00},
	'+':  {0x00, 0x18, 0x18, 0x7E, 0x18, 0x18, 0x00, 0x00},
	'.':  {0x00, 0x00, 0x00, 0x00, 0x00, 0x18, 0x18, 0x00},
	',':  {0x00, 0x00, 0x00, 0x00, 0x00, 0x18, 0x18, 0x30},
	'!':  {0x18, 0x18, 0x18, 0x18, 0x00, 0x00, 0x18, 0x00},
	'?':  {0x3C, 0x66, 0x06, 0x0C, 0x18, 0x00, 0x18, 0x00},
	'[':  {0x3C, 0x30, 0x30, 0x30, 0x30, 0x30, 0x3C, 0x00},
	']':  {0x3C, 0x0C, 0x0C, 0x0C, 0x0C, 0x0C, 0x3C, 0x00},
	'/':  {0x00, 0x06, 0x0C, 0x18, 0x30, 0x60, 0x00, 0x00},
	'\\': {0x00, 0x60, 0x30, 0x18, 0x0C, 0x06, 0x00, 0x00},
}

// Linux keycode to character mapping (simplified)
var keycodeToChar = map[uint16]byte{
	30: 'A', 48: 'B', 46: 'C', 32: 'D', 18: 'E',
	33: 'F', 34: 'G', 35: 'H', 23: 'I', 36: 'J',
	37: 'K', 38: 'L', 50: 'M', 49: 'N', 24: 'O',
	25: 'P', 16: 'Q', 19: 'R', 31: 'S', 20: 'T',
	22: 'U', 47: 'V', 17: 'W', 45: 'X', 21: 'Y',
	44: 'Z', 11: '0', 2: '1', 3: '2', 4: '3',
	5: '4', 6: '5', 7: '6', 8: '7', 9: '8', 10: '9',
	57: ' ', // Space
}

// Framebuffer represents an open framebuffer device
type Framebuffer struct {
	file       *os.File
	mem        []byte
	width      uint32
	height     uint32
	bpp        uint32
	lineLength uint32
}

func openFramebuffer(path string) (*Framebuffer, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open framebuffer: %w", err)
	}

	// Get variable screen info
	var vinfo fbVarScreenInfo
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), FBIOGET_VSCREENINFO, uintptr(unsafe.Pointer(&vinfo)))
	if errno != 0 {
		f.Close()
		return nil, fmt.Errorf("FBIOGET_VSCREENINFO: %v", errno)
	}

	// Get fixed screen info
	var finfo fbFixScreenInfo
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, f.Fd(), FBIOGET_FSCREENINFO, uintptr(unsafe.Pointer(&finfo)))
	if errno != 0 {
		f.Close()
		return nil, fmt.Errorf("FBIOGET_FSCREENINFO: %v", errno)
	}

	// Memory map the framebuffer
	screenSize := int(finfo.LineLength * vinfo.YRes)
	mem, err := unix.Mmap(int(f.Fd()), 0, screenSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("mmap framebuffer: %w", err)
	}

	return &Framebuffer{
		file:       f,
		mem:        mem,
		width:      vinfo.XRes,
		height:     vinfo.YRes,
		bpp:        vinfo.BitsPerPixel,
		lineLength: finfo.LineLength,
	}, nil
}

func (fb *Framebuffer) Close() error {
	if fb.mem != nil {
		unix.Munmap(fb.mem)
	}
	return fb.file.Close()
}

// SetPixel sets a pixel at (x, y) to the given RGBA color
func (fb *Framebuffer) SetPixel(x, y int, r, g, b, a uint8) {
	if x < 0 || x >= int(fb.width) || y < 0 || y >= int(fb.height) {
		return
	}

	offset := y*int(fb.lineLength) + x*int(fb.bpp/8)
	if offset+3 >= len(fb.mem) {
		return
	}

	// Assuming BGRA format (common for virtio-gpu)
	fb.mem[offset+0] = b
	fb.mem[offset+1] = g
	fb.mem[offset+2] = r
	fb.mem[offset+3] = a
}

// Clear fills the framebuffer with a solid color
func (fb *Framebuffer) Clear(r, g, b, a uint8) {
	for y := 0; y < int(fb.height); y++ {
		for x := 0; x < int(fb.width); x++ {
			fb.SetPixel(x, y, r, g, b, a)
		}
	}
}

// DrawRect draws a filled rectangle
func (fb *Framebuffer) DrawRect(x, y, w, h int, r, g, b, a uint8) {
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			fb.SetPixel(x+dx, y+dy, r, g, b, a)
		}
	}
}

// DrawCursor draws a simple cursor (crosshair) at the given position
func (fb *Framebuffer) DrawCursor(cx, cy int) {
	// Draw a white crosshair with black outline
	size := 10

	// Horizontal line (outline)
	for dx := -size; dx <= size; dx++ {
		fb.SetPixel(cx+dx, cy-1, 0, 0, 0, 255)
		fb.SetPixel(cx+dx, cy+1, 0, 0, 0, 255)
	}
	// Horizontal line (white)
	for dx := -size; dx <= size; dx++ {
		fb.SetPixel(cx+dx, cy, 255, 255, 255, 255)
	}

	// Vertical line (outline)
	for dy := -size; dy <= size; dy++ {
		fb.SetPixel(cx-1, cy+dy, 0, 0, 0, 255)
		fb.SetPixel(cx+1, cy+dy, 0, 0, 0, 255)
	}
	// Vertical line (white)
	for dy := -size; dy <= size; dy++ {
		fb.SetPixel(cx, cy+dy, 255, 255, 255, 255)
	}
}

// DrawChar draws a single character at the given position
func (fb *Framebuffer) DrawChar(x, y int, c byte, r, g, b uint8) {
	glyph, ok := font8x8[c]
	if !ok {
		return
	}

	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			if (glyph[row] & (0x80 >> col)) != 0 {
				fb.SetPixel(x+col, y+row, r, g, b, 255)
			}
		}
	}
}

// DrawText draws a string at the given position
func (fb *Framebuffer) DrawText(x, y int, text string, r, g, b uint8) {
	for i, c := range text {
		fb.DrawChar(x+i*8, y, byte(c), r, g, b)
	}
}

func TestFramebufferBasic(t *testing.T) {
	t.Logf("Testing basic framebuffer access")

	// Check if framebuffer device exists
	if _, err := os.Stat(fbDevice); os.IsNotExist(err) {
		t.Fatalf("Framebuffer device %s does not exist", fbDevice)
	}

	fb, err := openFramebuffer(fbDevice)
	if err != nil {
		t.Fatalf("Failed to open framebuffer: %v", err)
	}
	defer fb.Close()

	t.Logf("Framebuffer opened: %dx%d, %d bpp, line length %d",
		fb.width, fb.height, fb.bpp, fb.lineLength)

	// Clear to dark blue
	fb.Clear(20, 20, 60, 255)

	// Draw some test patterns
	fb.DrawRect(10, 10, 100, 50, 255, 0, 0, 255)  // Red rectangle
	fb.DrawRect(120, 10, 100, 50, 0, 255, 0, 255) // Green rectangle
	fb.DrawRect(230, 10, 100, 50, 0, 0, 255, 255) // Blue rectangle

	// Draw text
	fb.DrawText(10, 80, "GPU BRINGUP TEST", 255, 255, 255)

	t.Logf("Basic framebuffer test passed")
}

func TestInputDevice(t *testing.T) {
	t.Logf("Testing input device access")

	// Check if input device exists
	if _, err := os.Stat(inputDevice); os.IsNotExist(err) {
		t.Skipf("Input device %s does not exist", inputDevice)
	}

	f, err := os.Open(inputDevice)
	if err != nil {
		t.Fatalf("Failed to open input device: %v", err)
	}
	defer f.Close()

	t.Logf("Input device opened successfully")
}

func TestInteractiveDisplay(t *testing.T) {
	t.Logf("Starting interactive display test")

	// Open framebuffer
	fb, err := openFramebuffer(fbDevice)
	if err != nil {
		t.Fatalf("Failed to open framebuffer: %v", err)
	}
	defer fb.Close()

	t.Logf("Framebuffer opened: %dx%d", fb.width, fb.height)

	// Try to open input device (non-blocking)
	var inputFile *os.File
	if _, err := os.Stat(inputDevice); err == nil {
		inputFile, err = os.OpenFile(inputDevice, os.O_RDONLY|unix.O_NONBLOCK, 0)
		if err != nil {
			t.Logf("Warning: Failed to open input device: %v", err)
		}
	}
	if inputFile != nil {
		defer inputFile.Close()
	}

	// State
	cursorX := int(fb.width / 2)
	cursorY := int(fb.height / 2)
	pressedKeys := make([]byte, 0, 32)
	lastKeyTime := time.Now()

	// Main loop - run for 5 seconds
	startTime := time.Now()
	duration := 5 * time.Second
	frameCount := 0

	for time.Since(startTime) < duration {
		// Process input events (non-blocking)
		if inputFile != nil {
			var ev inputEvent
			evSize := int(unsafe.Sizeof(ev))
			buf := make([]byte, evSize)

			for {
				n, err := inputFile.Read(buf)
				if err != nil || n != evSize {
					break
				}

				ev.Type = binary.LittleEndian.Uint16(buf[16:18])
				ev.Code = binary.LittleEndian.Uint16(buf[18:20])
				ev.Value = int32(binary.LittleEndian.Uint32(buf[20:24]))

				switch ev.Type {
				case EV_ABS:
					// Handle absolute position (tablet mode)
					switch ev.Code {
					case ABS_X:
						// Scale from 0-32767 to screen width
						cursorX = int(int64(ev.Value) * int64(fb.width) / 32768)
					case ABS_Y:
						// Scale from 0-32767 to screen height
						cursorY = int(int64(ev.Value) * int64(fb.height) / 32768)
					}
				case EV_KEY:
					if ev.Value == 1 { // Key pressed
						if c, ok := keycodeToChar[ev.Code]; ok {
							pressedKeys = append(pressedKeys, c)
							if len(pressedKeys) > 30 {
								pressedKeys = pressedKeys[1:]
							}
							lastKeyTime = time.Now()
						}
					}
				}
			}
		}

		// Clear screen
		fb.Clear(20, 20, 60, 255)

		// Draw header
		fb.DrawText(10, 10, "GPU BRINGUP TEST - VIRTIO GPU + INPUT", 255, 255, 0)
		elapsed := time.Since(startTime).Seconds()
		remaining := duration.Seconds() - elapsed
		fb.DrawText(10, 25, fmt.Sprintf("TIME: %.1fS REMAINING: %.1fS", elapsed, remaining), 200, 200, 200)

		// Draw cursor position
		fb.DrawText(10, 50, fmt.Sprintf("CURSOR: %d, %d", cursorX, cursorY), 200, 200, 200)

		// Draw pressed keys
		fb.DrawText(10, 70, "KEYS:", 200, 200, 200)
		if len(pressedKeys) > 0 {
			fb.DrawText(50, 70, string(pressedKeys), 0, 255, 0)
		}

		// Clear keys after 2 seconds of inactivity
		if time.Since(lastKeyTime) > 2*time.Second {
			pressedKeys = pressedKeys[:0]
		}

		// Draw cursor
		fb.DrawCursor(cursorX, cursorY)

		// Draw frame count
		fb.DrawText(10, int(fb.height)-20, fmt.Sprintf("FRAME: %d", frameCount), 150, 150, 150)

		frameCount++

		// Small delay to avoid busy-waiting
		time.Sleep(16 * time.Millisecond) // ~60 FPS
	}

	t.Logf("Interactive test completed: %d frames rendered", frameCount)
}
