package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"j5.nz/cc/ccvmd"
	ccclient "j5.nz/cc/client"
	"j5.nz/cc/internal/rfb"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
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
	if len(args) > 0 && args[0] == "run" {
		return runVM(args[1:])
	}
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
	return fmt.Errorf("usage: glass run [OPTIONS] IMAGE\n       glass [-timeout DURATION] <probe|capture|resize|clipboard-set|clipboard-get|type|key|move|click|wait-pixel> ADDRESS ...")
}

func runVM(args []string) (retErr error) {
	fs := flag.NewFlagSet("glass run", flag.ContinueOnError)
	name := fs.String("name", "glass", "VM name")
	cacheDir := fs.String("cache-dir", "", "Image and runtime cache directory")
	vncListen := fs.String("vnc-listen", "127.0.0.1:0", "VNC listen address")
	display := fs.String("display", "1440x900", "Initial display size WIDTHxHEIGHT")
	initSystem := fs.String("init", "systemd", "Guest init system")
	memoryMB := fs.Uint64("memory-mb", 8192, "Guest memory in MiB")
	cpus := fs.Int("cpus", 4, "Guest CPU count")
	network := fs.Bool("network", true, "Enable isolated networking with outbound internet access")
	bootTimeout := fs.Duration("boot-timeout", 10*time.Minute, "VM preparation and boot timeout")
	dmesg := fs.Bool("dmesg", false, "Forward the guest kernel log")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: glass run [OPTIONS] IMAGE")
	}
	if *name == "" {
		return fmt.Errorf("VM name cannot be empty")
	}
	if *memoryMB == 0 {
		return fmt.Errorf("memory must be greater than zero")
	}
	if *cpus <= 0 {
		return fmt.Errorf("CPU count must be greater than zero")
	}
	width, height, err := parseDisplaySize(*display)
	if err != nil {
		return err
	}

	ready := make(chan ccclient.ServerHello, 1)
	serverDone := make(chan error, 1)
	serverArgs := []string{"-addr", "127.0.0.1:0"}
	if strings.TrimSpace(*cacheDir) != "" {
		serverArgs = append(serverArgs, "-cache-dir", *cacheDir)
	}
	go func() {
		_, err := ccvmd.RunServer(serverArgs, ccvmd.ServerOptions{
			Kind:          "glass",
			StartupWriter: io.Discard,
			OnStartup: func(hello ccclient.ServerHello) error {
				ready <- hello
				return nil
			},
		})
		serverDone <- err
	}()

	var hello ccclient.ServerHello
	select {
	case hello = <-ready:
	case err := <-serverDone:
		return fmt.Errorf("start embedded VM backend: %w", err)
	}
	if hello.Addr == "" {
		return fmt.Errorf("embedded VM backend did not publish an address")
	}
	scheme := hello.Scheme
	if scheme == "" {
		scheme = "http"
	}
	api := ccclient.NewClient(scheme+"://"+hello.Addr, nil)
	lifetimeContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	serverFinished := false
	defer func() {
		if serverFinished {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		shutdownErr := api.ShutdownContext(ctx)
		cancel()
		if shutdownErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("stop embedded VM backend: %w", shutdownErr))
			return
		}
		select {
		case err := <-serverDone:
			serverFinished = true
			if err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("embedded VM backend shutdown: %w", err))
			}
		case <-time.After(15 * time.Second):
			retErr = errors.Join(retErr, fmt.Errorf("embedded VM backend did not stop"))
		}
	}()

	var networkConfig *ccclient.NetworkConfig
	if *network {
		networkConfig = &ccclient.NetworkConfig{
			Enabled:       true,
			AllowInternet: true,
		}
	}
	var lastBootMessage string
	state, err := api.CreateInstanceStreamWithIDContext(lifetimeContext, *name, ccclient.CreateInstanceRequest{
		Image:      fs.Arg(0),
		InitSystem: *initSystem,
		Network:    networkConfig,
		Display: &ccclient.DisplayConfig{
			Width:     uint32(width),
			Height:    uint32(height),
			VNCListen: *vncListen,
		},
		MemoryMB:       *memoryMB,
		CPUs:           *cpus,
		Dmesg:          *dmesg,
		TimeoutSeconds: bootTimeout.Seconds(),
	}, func(event ccclient.BootEvent) error {
		message := strings.TrimSpace(event.Message)
		if message != "" && message != lastBootMessage {
			fmt.Fprintln(os.Stderr, message)
			lastBootMessage = message
		}
		return nil
	})
	if err != nil {
		if lifetimeContext.Err() != nil {
			fmt.Fprintln(os.Stderr, "Stopping Glass VM...")
			return nil
		}
		return fmt.Errorf("boot %q: %w", fs.Arg(0), err)
	}
	if state.Display == nil || state.Display.VNCAddress == "" {
		return fmt.Errorf("VM started without a VNC endpoint")
	}

	fmt.Printf("VNC listening on %s\n", state.Display.VNCAddress)
	fmt.Printf("VM %q is running with %d CPUs and %d MiB RAM", state.ID, state.CPUs, state.MemoryMB)
	if state.NetworkIPv4 != "" {
		fmt.Printf(" at %s", state.NetworkIPv4)
	}
	fmt.Println()
	fmt.Println("Press Ctrl-C to stop the VM.")

	statusTicker := time.NewTicker(time.Second)
	defer statusTicker.Stop()
	for {
		select {
		case <-lifetimeContext.Done():
			fmt.Fprintln(os.Stderr, "Stopping Glass VM...")
			return nil
		case err := <-serverDone:
			serverFinished = true
			if err == nil {
				return fmt.Errorf("embedded VM backend stopped unexpectedly")
			}
			return fmt.Errorf("embedded VM backend stopped: %w", err)
		case <-statusTicker.C:
			current, err := api.InstanceStatusOfContext(lifetimeContext, *name)
			if err != nil {
				if lifetimeContext.Err() != nil {
					fmt.Fprintln(os.Stderr, "Stopping Glass VM...")
					return nil
				}
				return fmt.Errorf("check VM status: %w", err)
			}
			if current.Status != "running" {
				detail := current.Error
				if detail == "" {
					detail = current.ExitReason
				}
				if detail == "" {
					detail = "no failure detail was reported"
				}
				return fmt.Errorf("VM entered %q state: %s", current.Status, detail)
			}
		}
	}
}

func parseDisplaySize(value string) (int, int, error) {
	widthText, heightText, ok := strings.Cut(strings.ToLower(strings.TrimSpace(value)), "x")
	if !ok {
		return 0, 0, fmt.Errorf("display size %q must be WIDTHxHEIGHT", value)
	}
	return parseDimensions(widthText, heightText)
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
