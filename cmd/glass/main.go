package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"j5.nz/cc/internal/rfb"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "glass:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	global := flag.NewFlagSet("glass", flag.ContinueOnError)
	timeout := global.Duration("timeout", 30*time.Second, "Operation timeout")
	if err := global.Parse(args); err != nil {
		return err
	}
	args = global.Args()
	if len(args) < 2 {
		return usageError()
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client, err := rfb.Dial(ctx, args[1])
	if err != nil {
		return err
	}
	defer client.Close()

	switch args[0] {
	case "probe":
		if len(args) != 2 {
			return fmt.Errorf("usage: glass probe ADDRESS")
		}
		width, height := client.Size()
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"name":   client.Name(),
			"width":  width,
			"height": height,
		})
	case "capture":
		if len(args) != 3 {
			return fmt.Errorf("usage: glass capture ADDRESS OUTPUT.png")
		}
		return client.CapturePNG(ctx, args[2])
	case "resize":
		if len(args) != 4 {
			return fmt.Errorf("usage: glass resize ADDRESS WIDTH HEIGHT")
		}
		width, height, err := parseDimensions(args[2], args[3])
		if err != nil {
			return err
		}
		if err := client.Resize(width, height); err != nil {
			return err
		}
		if _, err := client.ReadUpdate(ctx); err != nil {
			return err
		}
		actualWidth, actualHeight := client.Size()
		if actualWidth != width || actualHeight != height {
			return fmt.Errorf("server selected %dx%d after requesting %dx%d", actualWidth, actualHeight, width, height)
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]int{
			"width":  actualWidth,
			"height": actualHeight,
		})
	case "clipboard-set":
		if len(args) != 3 {
			return fmt.Errorf("usage: glass clipboard-set ADDRESS TEXT")
		}
		return client.SetClipboard(args[2])
	case "clipboard-get":
		if len(args) != 2 {
			return fmt.Errorf("usage: glass clipboard-get ADDRESS")
		}
		width, height := client.Size()
		if err := client.RequestUpdate(image.Rect(0, 0, width, height), false); err != nil {
			return err
		}
		if _, err := client.ReadUpdate(ctx); err != nil {
			return err
		}
		_, err := io.WriteString(os.Stdout, client.Clipboard())
		return err
	case "type":
		if len(args) != 3 {
			return fmt.Errorf("usage: glass type ADDRESS TEXT")
		}
		return client.Type(args[2])
	case "key":
		if len(args) != 3 {
			return fmt.Errorf("usage: glass key ADDRESS KEYSYM")
		}
		keysym, err := parseKeysym(args[2])
		if err != nil {
			return err
		}
		return client.Press(keysym)
	case "move":
		if len(args) != 4 {
			return fmt.Errorf("usage: glass move ADDRESS X Y")
		}
		x, y, err := parsePoint(args[2], args[3])
		if err != nil {
			return err
		}
		return client.Pointer(x, y, 0)
	case "click":
		if len(args) != 4 && len(args) != 5 {
			return fmt.Errorf("usage: glass click ADDRESS X Y [BUTTON]")
		}
		x, y, err := parsePoint(args[2], args[3])
		if err != nil {
			return err
		}
		button := uint8(1)
		if len(args) == 5 {
			value, err := strconv.ParseUint(args[4], 10, 3)
			if err != nil || value == 0 {
				return fmt.Errorf("invalid button %q", args[4])
			}
			button = 1 << (value - 1)
		}
		return client.Click(x, y, button)
	case "wait-pixel":
		if len(args) != 5 {
			return fmt.Errorf("usage: glass wait-pixel ADDRESS X Y RRGGBB")
		}
		x, y, err := parsePoint(args[2], args[3])
		if err != nil {
			return err
		}
		color, err := parseColor(args[4])
		if err != nil {
			return err
		}
		return waitPixel(ctx, client, int(x), int(y), color)
	default:
		return usageError()
	}
}

func usageError() error {
	return fmt.Errorf("usage: glass [-timeout DURATION] <probe|capture|resize|clipboard-set|clipboard-get|type|key|move|click|wait-pixel> ADDRESS ...")
}

func parseDimensions(widthText, heightText string) (int, int, error) {
	width, err := strconv.Atoi(widthText)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid display width %q", widthText)
	}
	height, err := strconv.Atoi(heightText)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid display height %q", heightText)
	}
	if width <= 0 || height <= 0 || width > 8192 || height > 8192 {
		return 0, 0, fmt.Errorf("invalid display dimensions %dx%d", width, height)
	}
	return width, height, nil
}

func parsePoint(xText, yText string) (uint16, uint16, error) {
	x, err := strconv.ParseUint(xText, 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid x coordinate %q", xText)
	}
	y, err := strconv.ParseUint(yText, 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid y coordinate %q", yText)
	}
	return uint16(x), uint16(y), nil
}

func parseKeysym(value string) (uint32, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "enter", "return":
		return 0xff0d, nil
	case "escape", "esc":
		return 0xff1b, nil
	case "tab":
		return 0xff09, nil
	case "backspace":
		return 0xff08, nil
	case "up":
		return 0xff52, nil
	case "down":
		return 0xff54, nil
	case "left":
		return 0xff51, nil
	case "right":
		return 0xff53, nil
	}
	if len([]rune(value)) == 1 {
		return uint32([]rune(value)[0]), nil
	}
	parsed, err := strconv.ParseUint(value, 0, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid keysym %q", value)
	}
	return uint32(parsed), nil
}

func parseColor(value string) ([3]byte, error) {
	var color [3]byte
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != 3 {
		return color, fmt.Errorf("color %q must be RRGGBB", value)
	}
	copy(color[:], raw)
	return color, nil
}

func waitPixel(ctx context.Context, client *rfb.Client, x, y int, want [3]byte) error {
	width, height := client.Size()
	if x < 0 || y < 0 || x >= width || y >= height {
		return fmt.Errorf("pixel %d,%d is outside %dx%d display", x, y, width, height)
	}
	incremental := false
	for {
		if err := client.RequestUpdate(image.Rect(0, 0, width, height), incremental); err != nil {
			return err
		}
		if _, err := client.ReadUpdate(ctx); err != nil {
			return err
		}
		red, green, blue, _ := client.Pixel(x, y)
		if red == want[0] && green == want[1] && blue == want[2] {
			return nil
		}
		incremental = true
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for pixel %d,%d=%02x%02x%02x: %w", x, y, want[0], want[1], want[2], ctx.Err())
		default:
		}
	}
}
