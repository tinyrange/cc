package vm

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

const (
	runtimeBootImage    = "alpine-runtime-boot-test"
	runtimeBootAltImage = "alpine-runtime-boot-test-alt"
)

type runtimeBootEnv struct {
	backend      Backend
	images       *oci.Store
	imageName    string
	altImageName string
	memoryMB     uint64
	caps         client.CapabilitiesResponse
}

type runtimeImageAdder interface {
	AddImage(context.Context, string, *oci.Image) error
}

func TestRuntimeBootsLinuxAndRunsOneShotCommand(t *testing.T) {
	env := newRuntimeBootEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), runtimeBootTimeout())
	defer cancel()

	resp, err := env.backend.Run(ctx, client.RunRequest{
		Image:    env.imageName,
		MemoryMB: env.memoryMB,
		CPUs:     1,
		Command: []string{
			"sh",
			"-lc",
			"set -eu; printf 'runtime-one-shot\n'; cat /proc/sys/kernel/ostype; test -r /proc/1/cmdline; cat /etc/alpine-release; printf 'machine=%s\n' \"$(uname -m)\"",
		},
	})
	requireRunResponse(t, resp, err, 0)
	requireGuestOutput(t, resp.Output, "runtime-one-shot", "Linux", "machine=")
}

func TestRuntimeOneShotReportsNonZeroExitAndStderr(t *testing.T) {
	env := newRuntimeBootEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), runtimeBootTimeout())
	defer cancel()

	resp, err := env.backend.Run(ctx, client.RunRequest{
		Image:    env.imageName,
		MemoryMB: env.memoryMB,
		CPUs:     1,
		Command: []string{
			"sh",
			"-lc",
			"printf 'stdout-before-fail\n'; printf 'stderr-before-fail\n' >&2; exit 37",
		},
	})
	requireRunResponse(t, resp, err, 37)
	requireGuestOutput(t, resp.Output, "stdout-before-fail", "stderr-before-fail")
}

func TestRuntimeOneShotDmesgIncludesTranscript(t *testing.T) {
	env := newRuntimeBootEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), runtimeBootTimeout())
	defer cancel()

	resp, err := env.backend.Run(ctx, client.RunRequest{
		Image:    env.imageName,
		MemoryMB: env.memoryMB,
		CPUs:     1,
		Dmesg:    true,
		Command: []string{
			"sh",
			"-lc",
			"set -eu; printf 'runtime-dmesg-one-shot\n'; true",
		},
	})
	requireRunResponse(t, resp, err, 0)
	requireGuestOutput(t, resp.Output, "runtime-dmesg-one-shot", "ccx3-init")
}

func TestRuntimeBootsLinuxWithExt4Root(t *testing.T) {
	t.Setenv("CCX3_ROOTFS_IMAGE_TYPE", "ext4")
	env := newRuntimeBootEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), runtimeBootTimeout())
	defer cancel()

	resp, err := env.backend.Run(ctx, client.RunRequest{
		Image:    env.imageName,
		MemoryMB: env.memoryMB,
		CPUs:     1,
		Command: []string{
			"sh",
			"-lc",
			"set -eu; awk '$5 == \"/\" { for (i = 1; i <= NF; i++) if ($i == \"-\") { print $(i+1); exit } }' /proc/self/mountinfo",
		},
	})
	requireRunResponse(t, resp, err, 0)
	if strings.TrimSpace(resp.Output) != "ext4" {
		t.Fatalf("root filesystem type = %q, want ext4\noutput:\n%s", strings.TrimSpace(resp.Output), resp.Output)
	}
}

func TestRuntimeBootsOpenBSDBuiltinImage(t *testing.T) {
	ctx, inst := bootManagedBSDRuntimeContract(t, managedBSDRuntimeBootCase{
		name:     "OpenBSD",
		envVar:   "CC_TEST_OPENBSD_KVM",
		image:    "@openbsd",
		memoryMB: 768,
		timeout:  120 * time.Second,
		guestOS:  "OpenBSD",
		label:    "openbsd",
	})

	inputs := make(chan client.ExecInput, 2)
	var interactiveControl strings.Builder
	var sentInteractiveInput bool
	var interactiveExit *int
	if err := inst.ExecStream(ctx, client.ExecRequest{
		Command:   []string{"sh", "-c", "printf 'ready:%s\\n' \"$PWD\" >&3; IFS= read -r line; eval \"$line\""},
		WorkDir:   "/tmp",
		ControlFD: true,
	}, inputs, func(event client.ExecEvent) error {
		switch event.Kind {
		case "control":
			interactiveControl.WriteString(event.Output)
			if !sentInteractiveInput && strings.Contains(event.Output, "ready:/tmp") {
				sentInteractiveInput = true
				inputs <- client.ExecInput{Kind: "stdin", Data: []byte("printf 'done:%s\\n' \"$PWD\" >&3\n")}
				close(inputs)
			}
		case "exit":
			code := event.ExitCode
			interactiveExit = &code
		}
		return nil
	}); err != nil {
		t.Fatalf("OpenBSD runtime interactive control fd exec: %v", err)
	}
	if !sentInteractiveInput {
		t.Fatalf("OpenBSD interactive control fd never reported ready; control=%q", interactiveControl.String())
	}
	if interactiveExit == nil || *interactiveExit != 0 {
		t.Fatalf("OpenBSD interactive control fd exit = %v, want 0", interactiveExit)
	}
	if got := interactiveControl.String(); !strings.Contains(got, "ready:/tmp") || !strings.Contains(got, "done:/tmp") {
		t.Fatalf("OpenBSD interactive control fd output = %q, want ready and done", got)
	}
}

func TestRuntimeBootsFreeBSDBuiltinImage(t *testing.T) {
	_, _ = bootManagedBSDRuntimeContract(t, managedBSDRuntimeBootCase{
		name:     "FreeBSD",
		envVar:   "CC_TEST_FREEBSD_KVM",
		image:    "@freebsd",
		memoryMB: 1024,
		timeout:  180 * time.Second,
		guestOS:  "FreeBSD",
		label:    "freebsd",
	})
}

type managedBSDRuntimeBootCase struct {
	name     string
	envVar   string
	image    string
	memoryMB uint64
	timeout  time.Duration
	guestOS  string
	label    string
}

func bootManagedBSDRuntimeContract(t *testing.T, tc managedBSDRuntimeBootCase) (context.Context, Instance) {
	t.Helper()
	if os.Getenv(tc.envVar) == "" {
		t.Skipf("set %s=1 to run %s runtime boot test", tc.envVar, tc.name)
	}
	if err := Supports(); err != nil {
		t.Skipf("VM runtime unsupported on this host: %v", err)
	}
	cacheRoot := runtimeBootCacheRoot(t)
	backend := NewRuntimeBackend(nil, nil, filepath.Join(cacheRoot, "guestinit"))
	ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
	t.Cleanup(cancel)
	inst, err := backend.StartStream(ctx, client.CreateInstanceRequest{
		Image:    tc.image,
		MemoryMB: tc.memoryMB,
	}, nil)
	if err != nil {
		t.Fatalf("boot %s runtime: %v", tc.name, err)
	}
	t.Cleanup(func() { _ = inst.Close() })

	resp, err := inst.Exec(ctx, client.ExecRequest{
		Command: []string{"sh", "-c", fmt.Sprintf("printf '%s-runtime:'; printf %%s \"$(uname -s)\"; printf ':copy:'; cat", tc.label)},
		WorkDir: "/tmp",
		Stdin:   []byte("stdin-ok"),
	})
	if err != nil {
		t.Fatalf("%s runtime exec: %v", tc.name, err)
	}
	normalized := strings.ReplaceAll(strings.TrimSpace(resp.Output), "\n", "")
	expectedOutput := fmt.Sprintf("%s-runtime:%s:copy:stdin-ok", tc.label, tc.guestOS)
	if resp.ExitCode != 0 || normalized != expectedOutput {
		t.Fatalf("%s exec response = code %d output %q, want %q", tc.name, resp.ExitCode, resp.Output, expectedOutput)
	}

	var controlOutput string
	var controlExit *int
	if err := inst.ExecStream(ctx, client.ExecRequest{
		Command:   []string{"sh", "-c", fmt.Sprintf("printf '%s-control:%%s\\n' \"$PWD\" >&3", tc.label)},
		WorkDir:   "/tmp",
		ControlFD: true,
	}, nil, func(event client.ExecEvent) error {
		switch event.Kind {
		case "control":
			controlOutput += event.Output
		case "exit":
			code := event.ExitCode
			controlExit = &code
		}
		return nil
	}); err != nil {
		t.Fatalf("%s runtime control fd exec: %v", tc.name, err)
	}
	if controlExit == nil || *controlExit != 0 {
		t.Fatalf("%s control fd exit = %v, want 0", tc.name, controlExit)
	}
	expectedControl := fmt.Sprintf("%s-control:/tmp", tc.label)
	if strings.TrimSpace(controlOutput) != expectedControl {
		t.Fatalf("%s control fd output = %q, want %s", tc.name, controlOutput, expectedControl)
	}

	flusher, ok := inst.(instanceFlushProvider)
	if !ok {
		t.Fatalf("%s runtime does not expose flush", tc.name)
	}
	if err := flusher.Flush(ctx); err != nil {
		t.Fatalf("%s runtime flush: %v", tc.name, err)
	}
	return ctx, inst
}

func TestRuntimeRejectsInvalidRequests(t *testing.T) {
	env := newRuntimeBootEnv(t)
	for _, tc := range []struct {
		name string
		req  client.RunRequest
		want string
	}{
		{
			name: "missing image",
			req: client.RunRequest{
				MemoryMB: env.memoryMB,
				CPUs:     1,
				Command:  []string{"sh", "-lc", "true"},
			},
			want: "image",
		},
		{
			name: "relative workdir",
			req: client.RunRequest{
				Image:    env.imageName,
				MemoryMB: env.memoryMB,
				CPUs:     1,
				WorkDir:  "relative",
				Command:  []string{"sh", "-lc", "true"},
			},
			want: "workdir must be absolute",
		},
		{
			name: "invalid user",
			req: client.RunRequest{
				Image:    env.imageName,
				MemoryMB: env.memoryMB,
				CPUs:     1,
				User:     "daemon",
				Command:  []string{"sh", "-lc", "true"},
			},
			want: "user",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), runtimeBootTimeout())
			defer cancel()
			resp, err := env.backend.Run(ctx, tc.req)
			if err == nil && resp.ExitCode == 0 {
				t.Fatalf("invalid request unexpectedly succeeded: %+v", resp)
			}
			if err != nil {
				requireErrorContains(t, err, tc.want)
			}
		})
	}
}

func TestRuntimeBootsPersistentLinuxAndExecsCommands(t *testing.T) {
	env := newRuntimeBootEnv(t)
	inst := startRuntimeInstance(t, env, client.CreateInstanceRequest{})
	defer inst.Close()

	first := execInRuntime(t, inst, []string{
		"sh",
		"-lc",
		"set -eu; printf 'runtime-persistent\n'; cat /proc/sys/kernel/ostype; test -d /sys/kernel; cat /etc/alpine-release",
	})
	requireGuestOutput(t, first.Output, "runtime-persistent", "Linux")

	second := execInRuntime(t, inst, []string{
		"sh",
		"-lc",
		"set -eu; printf '%s\n' $((21 + 21)); printf persisted >/tmp/runtime-test; cat /tmp/runtime-test",
	})
	requireGuestOutput(t, second.Output, "42", "persisted")
}

func TestRuntimePersistentRejectsRuntimeMountConflicts(t *testing.T) {
	env := newRuntimeBootEnv(t)
	sourceA := t.TempDir()
	sourceB := t.TempDir()
	mustWriteFile(t, filepath.Join(sourceA, "a.txt"), "a")
	mustWriteFile(t, filepath.Join(sourceB, "b.txt"), "b")

	inst := startRuntimeInstance(t, env, client.CreateInstanceRequest{})
	defer inst.Close()
	adder, ok := inst.(runtimeImageAdder)
	if !ok {
		t.Fatalf("runtime instance does not support image mounts")
	}

	share := client.ShareMount{Source: sourceA, Mount: "/mnt/runtime", Writable: true}
	addShareWithTimeout(t, inst, share)
	addShareWithTimeout(t, inst, share)

	conflictingShare := share
	conflictingShare.Source = sourceB
	err := addShareExpectError(t, inst, conflictingShare)
	requireErrorContains(t, err, `share mount "/mnt/runtime" already exists`)

	err = addImageExpectError(t, adder, share.Mount, env.openImage(t, env.imageName))
	requireErrorContains(t, err, `already in use`)

	imageMount := "/.ccx3/images/conflict"
	addImageWithTimeout(t, adder, imageMount, env.openImage(t, env.imageName))
	addImageWithTimeout(t, adder, imageMount, env.openImage(t, env.imageName))

	err = addImageExpectError(t, adder, imageMount, env.openImage(t, env.altImageName))
	requireErrorContains(t, err, `image mount "/.ccx3/images/conflict" already exists`)

	imagePathShare := client.ShareMount{Source: sourceB, Mount: imageMount, Writable: false}
	err = addShareExpectError(t, inst, imagePathShare)
	requireErrorContains(t, err, `already in use`)
}

func TestRuntimePersistentCancelAndCloseDuringLongRunningExec(t *testing.T) {
	env := newRuntimeBootEnv(t)
	inst := startRuntimeInstance(t, env, client.CreateInstanceRequest{})
	closed := false
	defer func() {
		if !closed {
			_ = inst.Close()
		}
	}()

	execCtx, cancelExec := context.WithCancel(context.Background())
	defer cancelExec()
	ready := make(chan struct{})
	done := make(chan error, 1)
	var readyOnce sync.Once
	var output bytes.Buffer
	go func() {
		done <- inst.ExecStream(execCtx, client.ExecRequest{
			Command: []string{"sh", "-lc", "set -eu; printf 'long-running-ready\n'; sleep 30; printf 'should-not-print\n'"},
		}, nil, func(event client.ExecEvent) error {
			if event.Kind == "stdout" || event.Kind == "stderr" {
				writeExecEventOutput(&output, event)
				if strings.Contains(output.String(), "long-running-ready") {
					readyOnce.Do(func() { close(ready) })
				}
			}
			return nil
		})
	}()

	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("long-running exec returned before readiness marker: %v\noutput:\n%s", err, output.String())
	case <-time.After(runtimeExecTimeout()):
		t.Fatalf("timed out waiting for long-running exec readiness\noutput:\n%s", output.String())
	}

	cancelExec()
	if err := inst.Close(); err != nil {
		t.Fatalf("close persistent guest during canceled exec: %v", err)
	}
	closed = true

	select {
	case err := <-done:
		requireErrorContains(t, err, context.Canceled.Error())
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for canceled exec to return\noutput:\n%s", output.String())
	}
	requireGuestOutput(t, output.String(), "long-running-ready")
	if strings.Contains(output.String(), "should-not-print") {
		t.Fatalf("long-running exec continued after cancellation\noutput:\n%s", output.String())
	}
}

func TestRuntimePersistentLinuxExercisesRuntimeFeatures(t *testing.T) {
	env := newRuntimeBootEnv(t)
	readOnlyShare := t.TempDir()
	writableShare := t.TempDir()
	lateShare := t.TempDir()
	mustWriteFile(t, filepath.Join(readOnlyShare, "host.txt"), "read-only-share\n")
	mustWriteFile(t, filepath.Join(lateShare, "late.txt"), "late-share\n")

	bootEvents := &bootEventRecorder{}
	inst := startRuntimeInstance(t, env, client.CreateInstanceRequest{
		Shares: []client.ShareMount{
			{Source: readOnlyShare, Mount: "/mnt/ro", Writable: false},
			{Source: writableShare, Mount: "/mnt/rw", Writable: true, Cache: "aggressive"},
		},
	}, bootEvents.Record)
	defer inst.Close()
	bootEvents.RequireKind(t, "status")

	shareEnv := execInRuntimeRequest(t, inst, client.ExecRequest{
		Command: []string{
			"sh",
			"-lc",
			"set -eu; printf 'runtime-features\n'; pwd; printf 'env=%s/%s\n' \"$RUNTIME_BASE\" \"$RUNTIME_EXTRA\"; cat /mnt/ro/host.txt; if printf denied >/mnt/ro/denied 2>/tmp/ro.err; then exit 41; fi; printf guest-write >/mnt/rw/guest.txt; cat /mnt/rw/guest.txt",
		},
		Env:     []string{"RUNTIME_BASE=base", "RUNTIME_EXTRA=extra"},
		WorkDir: "/tmp",
	})
	requireGuestOutput(t, shareEnv.Output, "runtime-features", "/tmp", "env=base/extra", "read-only-share", "guest-write")
	if got := mustReadHostFile(t, filepath.Join(writableShare, "guest.txt")); got != "guest-write" {
		t.Fatalf("writable share did not receive guest write: %q", got)
	}

	addShareWithTimeout(t, inst, client.ShareMount{Source: lateShare, Mount: "/mnt/late", Writable: true})
	late := execInRuntime(t, inst, []string{
		"sh",
		"-lc",
		"set -eu; cat /mnt/late/late.txt; printf late-write >/mnt/late/from-guest.txt",
	})
	requireGuestOutput(t, late.Output, "late-share")
	if got := mustReadHostFile(t, filepath.Join(lateShare, "from-guest.txt")); got != "late-write" {
		t.Fatalf("runtime share did not receive guest write: %q", got)
	}

	user := execInRuntimeRequest(t, inst, client.ExecRequest{
		Command: []string{"sh", "-lc", "set -eu; printf 'uid=%s gid=%s\n' \"$(id -u)\" \"$(id -g)\"; printf user-write >/tmp/user-owned; test -s /tmp/user-owned"},
		User:    "1234:1235",
	})
	requireGuestOutput(t, user.Output, "uid=1234 gid=1235")

	rootMount := execInRuntime(t, inst, []string{
		"sh",
		"-lc",
		"set -eu; awk '$4 == \"/\" && $5 == \"/\" { found = 1 } END { exit found ? 0 : 1 }' /proc/self/mountinfo; printf 'root-mount-ok\n'",
	})
	requireGuestOutput(t, rootMount.Output, "root-mount-ok")

	alt := runInRuntimeInstance(t, env, inst, client.RunRequest{
		Image:   env.altImageName,
		Command: []string{"sh", "-lc", "set -eu; test -r /etc/alpine-release; printf 'image-run-in-instance\n'; pwd"},
		WorkDir: "/",
	})
	requireGuestOutput(t, alt.Output, "image-run-in-instance", "/")

	altStream := execInRuntimeInstanceStream(t, env, inst, client.ExecRequest{
		Image:   env.altImageName,
		Command: []string{"sh", "-lc", "set -eu; printf 'image-exec-stream\n'; cat /etc/alpine-release"},
	})
	requireGuestOutput(t, altStream, "image-exec-stream")

	streamOutput := execStreamInRuntime(t, inst, client.ExecRequest{
		Command: []string{"sh", "-lc", "set -eu; while IFS= read -r line; do printf 'line:%s\n' \"$line\"; done"},
	}, execStreamInput("alpha\n", "beta\n"), 0)
	requireGuestOutput(t, streamOutput, "line:alpha", "line:beta")

	ttyOutput := execStreamInRuntime(t, inst, client.ExecRequest{
		Command: []string{"sh", "-lc", "set -eu; stty size; printf 'tty-ok\n'"},
		TTY:     true,
		Cols:    77,
		Rows:    33,
	}, nil, 0)
	requireGuestOutput(t, ttyOutput, "33 77", "tty-ok")

	signalOutput := execStreamSignalOnOutput(t, inst)
	requireGuestOutput(t, signalOutput, "signal-ready", "got-term")

	runRuntimeControl(t, inst, client.ExecRequest{Kind: "fs_mkdir", Path: "/runtime-control", User: "0:0"}, 0)
	runRuntimeControl(t, inst, client.ExecRequest{Kind: "fs_write", Path: "/runtime-control/file.txt", Stdin: []byte("control-write"), User: "0:0"}, 0)
	archive := runRuntimeControl(t, inst, client.ExecRequest{Kind: "fs_archive", Path: "/runtime-control", User: "0:0"}, 0)
	requireTarFile(t, archive, "runtime-control/file.txt", "control-write")
	runRuntimeControl(t, inst, client.ExecRequest{
		Kind:      "fs_extract",
		Path:      "/runtime-control",
		Directory: true,
		Stdin:     tarPayload(t, map[string]string{"extract.txt": "extracted"}),
		User:      "0:0",
	}, 0)
	extracted := execInRuntime(t, inst, []string{"sh", "-lc", "set -eu; cat /runtime-control/extract.txt"})
	requireGuestOutput(t, extracted.Output, "extracted")

	writtenRoot := execInRuntimeRequest(t, inst, client.ExecRequest{
		Command: []string{"sh", "-lc", "set -eu; printf snapshot-root >/runtime-snapshot.txt; sync"},
		User:    "0:0",
	})
	if writtenRoot.ExitCode != 0 {
		t.Fatalf("write root snapshot fixture exited with %d", writtenRoot.ExitCode)
	}
	flushRuntimeInstance(t, inst)
	rootSnapshot, err := inst.(rootSnapshotProvider).RootSnapshot()
	if err != nil {
		t.Fatalf("snapshot guest root filesystem: %v", err)
	}
	if got := readImageFile(t, rootSnapshot, "/runtime-snapshot.txt"); got != "snapshot-root" {
		t.Fatalf("root snapshot mismatch: %q", got)
	}

	imageSnapshot, err := inst.(imageSnapshotProvider).SnapshotImage(env.altImageName)
	if err != nil {
		t.Fatalf("snapshot mounted image: %v", err)
	}
	if got := readImageFile(t, imageSnapshot, "/etc/alpine-release"); strings.TrimSpace(got) == "" {
		t.Fatalf("mounted image snapshot did not include Alpine release")
	}

	stats := inst.(virtioFSStatsProvider).VirtioFSStats()
	if len(stats) == 0 {
		t.Fatalf("expected virtio-fs stats after filesystem activity")
	}
	var fuseRequests uint64
	for _, stat := range stats {
		fuseRequests += stat.FUSERequests
	}
	if fuseRequests == 0 {
		t.Fatalf("expected virtio-fs FUSE requests after filesystem activity: %+v", stats)
	}
}

func TestRuntimeBootsLinuxWithVirtioDevicesNetworkAndSMP(t *testing.T) {
	env := newRuntimeBootEnv(t)
	cpus := 1
	if runtimeBootContains(env.caps.ResourceLimits, "cpus") {
		cpus = 2
	}
	networkEnabled := runtimeBootContains(env.caps.NetworkModes, "user")
	var network *client.NetworkConfig
	if networkEnabled {
		network = &client.NetworkConfig{Enabled: true}
	}

	inst := startRuntimeInstance(t, env, client.CreateInstanceRequest{
		CPUs:    cpus,
		Network: network,
	})
	defer inst.Close()

	command := "set -eu; printf 'runtime-devices\n'; cpus=$(grep -c '^processor' /proc/cpuinfo); test \"$cpus\" -ge " + strconv.Itoa(cpus) + "; printf 'cpus=%s\n' \"$cpus\"; dd if=/dev/hwrng of=/tmp/runtime-rng bs=16 count=1 2>/dev/null; test \"$(wc -c </tmp/runtime-rng)\" = 16; printf 'rng=ok\n'"
	if networkEnabled {
		command += "; test -d /sys/class/net/eth0; mac=$(cat /sys/class/net/eth0/address); test -n \"$mac\"; grep -q '^nameserver 10.42.0.1$' /etc/resolv.conf; printf 'net=%s\n' \"$mac\""
	}
	resp := execInRuntimeRequest(t, inst, client.ExecRequest{
		Command: []string{"sh", "-lc", command},
		User:    "0:0",
	})
	requireGuestOutput(t, resp.Output, "runtime-devices", "cpus=", "rng=ok")
	if networkEnabled {
		requireGuestOutput(t, resp.Output, "net=")
		ipProvider, ok := inst.(networkIPv4Provider)
		if !ok {
			t.Fatalf("network-enabled instance does not report an IPv4 address")
		}
		if ip := strings.TrimSpace(ipProvider.NetworkIPv4()); ip == "" {
			t.Fatalf("network-enabled instance reported an empty IPv4 address")
		}
	}
}

func TestRuntimeIsolatedGuestCanReachAllowedServiceProxyPort(t *testing.T) {
	env := newRuntimeBootEnv(t)
	if !runtimeBootContains(env.caps.NetworkModes, "user") {
		t.Skip("runtime backend does not support user networking")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen host proxy fixture: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/probe" {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, "proxy-ok\n")
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ln)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("host proxy fixture serve: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), runtimeBootTimeout())
	defer cancel()
	resp, err := env.backend.Run(ctx, client.RunRequest{
		Image:    env.imageName,
		MemoryMB: env.memoryMB,
		CPUs:     1,
		Network: &client.NetworkConfig{
			Enabled:                  true,
			AllowInternet:            true,
			BlockHostAccess:          true,
			AllowedServiceProxyPorts: []int{port},
		},
		Command: []string{
			"sh",
			"-lc",
			fmt.Sprintf("set -eu; busybox wget -T 5 -qO- http://10.42.0.100:%d/probe", port),
		},
	})
	requireRunResponse(t, resp, err, 0)
	requireGuestOutput(t, resp.Output, "proxy-ok")
}

func TestRuntimeIsolatedAllowedServiceProxyStreamsFlushedResponse(t *testing.T) {
	env := newRuntimeBootEnv(t)
	if !runtimeBootContains(env.caps.NetworkModes, "user") {
		t.Skip("runtime backend does not support user networking")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen streaming host proxy fixture: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/events" {
				http.NotFound(w, r)
				return
			}
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming unsupported", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: alpha\n\n")
			flusher.Flush()
			time.Sleep(100 * time.Millisecond)
			_, _ = io.WriteString(w, "data: omega\n\n")
			flusher.Flush()
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ln)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("streaming host proxy fixture serve: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), runtimeBootTimeout())
	defer cancel()
	resp, err := env.backend.Run(ctx, client.RunRequest{
		Image:    env.imageName,
		MemoryMB: env.memoryMB,
		CPUs:     1,
		Network: &client.NetworkConfig{
			Enabled:                  true,
			AllowInternet:            true,
			BlockHostAccess:          true,
			AllowedServiceProxyPorts: []int{port},
		},
		Command: []string{
			"sh",
			"-lc",
			fmt.Sprintf("set -eu; busybox wget -T 10 -qO- http://10.42.0.100:%d/events >/tmp/events; grep -q 'data: alpha' /tmp/events; grep -q 'data: omega' /tmp/events; printf 'stream-ok\\n'", port),
		},
	})
	requireRunResponse(t, resp, err, 0)
	requireGuestOutput(t, resp.Output, "stream-ok")
}

func TestRuntimeIsolatedRunningGuestCanReachDynamicallyAllowedServiceProxyPort(t *testing.T) {
	env := newRuntimeBootEnv(t)
	if !runtimeBootContains(env.caps.NetworkModes, "user") {
		t.Skip("runtime backend does not support user networking")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen dynamic host proxy fixture: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/probe" {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, "dynamic-proxy-ok\n")
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ln)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("dynamic host proxy fixture serve: %v", err)
		}
	})

	inst := startRuntimeInstance(t, env, client.CreateInstanceRequest{
		Network: &client.NetworkConfig{
			Enabled:         true,
			AllowInternet:   true,
			BlockHostAccess: true,
		},
	})
	defer inst.Close()
	allower, ok := inst.(serviceProxyPortAllower)
	if !ok {
		t.Fatalf("runtime instance does not support dynamic service proxy allowlist")
	}
	if err := allower.AllowServiceProxyPort(context.Background(), port); err != nil {
		t.Fatalf("allow service proxy port: %v", err)
	}

	resp := execInRuntime(t, inst, []string{
		"sh",
		"-lc",
		fmt.Sprintf("set -eu; busybox wget -T 5 -qO- http://10.42.0.100:%d/probe", port),
	})
	requireGuestOutput(t, resp.Output, "dynamic-proxy-ok")
}

func TestRuntimePersistentLinuxConsoleHistoryWithDmesg(t *testing.T) {
	env := newRuntimeBootEnv(t)
	inst := startRuntimeInstance(t, env, client.CreateInstanceRequest{Dmesg: true})
	defer inst.Close()

	execInRuntime(t, inst, []string{"sh", "-lc", "true"})
	historyProvider, ok := inst.(consoleHistoryProvider)
	if !ok {
		t.Fatalf("runtime instance does not expose console history")
	}
	history, err := historyProvider.ConsoleHistory(context.Background())
	if err != nil {
		t.Fatalf("read console history: %v", err)
	}
	requireGuestOutput(t, history, "ccx3-init")
}

func TestRuntimeBootsLinuxWithNestedVirtualizationWhenSupported(t *testing.T) {
	env := newRuntimeBootEnv(t)
	if !env.caps.SupportsNestedVirt {
		t.Skipf("nested virtualization is not supported on %s", env.caps.Host)
	}
	inst := startRuntimeInstance(t, env, client.CreateInstanceRequest{NestedVirt: true})
	defer inst.Close()

	resp := execInRuntime(t, inst, []string{
		"sh",
		"-lc",
		"set -eu; printf 'runtime-nested\n'; cat /proc/sys/kernel/ostype; test -r /proc/cpuinfo",
	})
	requireGuestOutput(t, resp.Output, "runtime-nested", "Linux")
}

func newRuntimeBootEnv(t *testing.T) *runtimeBootEnv {
	t.Helper()
	if err := Supports(); err != nil {
		t.Skipf("VM runtime unsupported on this host: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), runtimePrepareTimeout())
	defer cancel()

	root := runtimeBootRepoRoot()
	cacheRoot := runtimeBootCacheRoot(t)
	t.Setenv("CCX3_OCI_SHARED_CACHE_DIR", filepath.Join(cacheRoot, "oci-shared"))
	for _, name := range []string{
		"CCX3_WORKER_BOOT_SOCKET",
		"CCX3_WORKER_FS_SOCKET",
		"CCX3_WORKER_NET_SOCKET",
		"CCX3_WORKER_NET_IPV4",
		"CCX3_WORKER_NET_MAC",
	} {
		t.Setenv(name, "")
	}

	kernelManager := alpine.NewManager(filepath.Join(cacheRoot, "kernel"))
	if err := kernelManager.EnsureWithProgress(ctx, nil); err != nil {
		t.Fatalf("prepare Alpine Linux kernel: %v", err)
	}

	store := oci.NewStore(filepath.Join(t.TempDir(), "images"))
	fixture := filepath.Join(root, "fixtures", "alpine.simg")
	for _, name := range []string{runtimeBootImage, runtimeBootAltImage} {
		if _, err := store.Pull(ctx, name, fixture, oci.PullOptions{Architecture: "amd64"}); err != nil {
			t.Fatalf("import Alpine SIMG fixture as %s: %v", name, err)
		}
	}

	return &runtimeBootEnv{
		backend:      NewRuntimeBackend(kernelManager, store, filepath.Join(cacheRoot, "guestinit")),
		images:       store,
		imageName:    runtimeBootImage,
		altImageName: runtimeBootAltImage,
		memoryMB:     768,
		caps:         HostCapabilities(),
	}
}

func (e *runtimeBootEnv) openImage(t *testing.T, name string) *oci.Image {
	t.Helper()
	image, err := e.images.Open(name)
	if err != nil {
		t.Fatalf("open image %s: %v", name, err)
	}
	if image == nil || image.RootFS == nil {
		t.Fatalf("image %s has no root filesystem", name)
	}
	return image
}

func startRuntimeInstance(t *testing.T, env *runtimeBootEnv, req client.CreateInstanceRequest, onEvent ...func(client.BootEvent) error) Instance {
	t.Helper()
	if strings.TrimSpace(req.Image) == "" {
		req.Image = env.imageName
	}
	if req.MemoryMB == 0 {
		req.MemoryMB = env.memoryMB
	}
	if req.CPUs == 0 {
		req.CPUs = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeBootTimeout())
	defer cancel()
	var events func(client.BootEvent) error
	if len(onEvent) > 0 {
		events = onEvent[0]
	}
	inst, err := env.backend.StartStream(ctx, req, events)
	if err != nil {
		t.Fatalf("boot persistent Linux guest: %v", err)
	}
	return inst
}

func runInRuntimeInstance(t *testing.T, env *runtimeBootEnv, inst Instance, req client.RunRequest) client.ExecResponse {
	t.Helper()
	if req.MemoryMB == 0 {
		req.MemoryMB = env.memoryMB
	}
	if req.CPUs == 0 {
		req.CPUs = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeExecTimeout())
	defer cancel()
	resp, err := env.backend.RunInInstance(ctx, inst, env.imageName, req)
	if err != nil {
		t.Fatalf("run image %q in existing instance: %v\nconsole:\n%s", req.Image, err, runtimeConsoleHistory(inst))
	}
	if resp.ExitCode != 0 {
		t.Fatalf("run image %q exited with %d\noutput:\n%s", req.Image, resp.ExitCode, resp.Output)
	}
	return resp
}

func execInRuntimeInstanceStream(t *testing.T, env *runtimeBootEnv, inst Instance, req client.ExecRequest) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runtimeExecTimeout())
	defer cancel()
	var output bytes.Buffer
	var exitCode *int
	err := env.backend.ExecInInstanceStream(ctx, inst, env.imageName, req, nil, func(event client.ExecEvent) error {
		switch event.Kind {
		case "stdout", "stderr":
			writeExecEventOutput(&output, event)
		case "exit":
			code := event.ExitCode
			exitCode = &code
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stream exec image %q in existing instance: %v\noutput:\n%s\nconsole:\n%s", req.Image, err, output.String(), runtimeConsoleHistory(inst))
	}
	if exitCode == nil {
		t.Fatalf("stream exec image %q did not report an exit\noutput:\n%s", req.Image, output.String())
	}
	if *exitCode != 0 {
		t.Fatalf("stream exec image %q exited with %d\noutput:\n%s", req.Image, *exitCode, output.String())
	}
	return output.String()
}

func execInRuntime(t *testing.T, inst Instance, command []string) client.ExecResponse {
	t.Helper()
	return execInRuntimeRequest(t, inst, client.ExecRequest{Command: command})
}

func execInRuntimeRequest(t *testing.T, inst Instance, req client.ExecRequest) client.ExecResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runtimeExecTimeout())
	defer cancel()
	resp, err := inst.Exec(ctx, req)
	if err != nil {
		t.Fatalf("run guest command %q: %v\nconsole:\n%s", req.Command, err, runtimeConsoleHistory(inst))
	}
	if resp.ExitCode != 0 {
		t.Fatalf("guest command %q exited with %d\noutput:\n%s", req.Command, resp.ExitCode, resp.Output)
	}
	return resp
}

func execStreamInput(chunks ...string) <-chan client.ExecInput {
	inputs := make(chan client.ExecInput, len(chunks))
	for _, chunk := range chunks {
		inputs <- client.ExecInput{Kind: "stdin", Input: chunk}
	}
	close(inputs)
	return inputs
}

func execStreamInRuntime(t *testing.T, inst Instance, req client.ExecRequest, inputs <-chan client.ExecInput, wantExit int) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runtimeExecTimeout())
	defer cancel()
	var output bytes.Buffer
	var exitCode *int
	err := inst.ExecStream(ctx, req, inputs, func(event client.ExecEvent) error {
		switch event.Kind {
		case "stdout", "stderr":
			writeExecEventOutput(&output, event)
		case "exit":
			code := event.ExitCode
			exitCode = &code
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stream guest exec %q: %v\noutput:\n%s\nconsole:\n%s", req.Command, err, output.String(), runtimeConsoleHistory(inst))
	}
	if exitCode == nil {
		t.Fatalf("stream guest exec %q did not report an exit\noutput:\n%s", req.Command, output.String())
	}
	if *exitCode != wantExit {
		t.Fatalf("stream guest exec %q exited with %d, want %d\noutput:\n%s", req.Command, *exitCode, wantExit, output.String())
	}
	return output.String()
}

func execStreamSignalOnOutput(t *testing.T, inst Instance) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runtimeExecTimeout())
	defer cancel()
	inputs := make(chan client.ExecInput, 1)
	var output bytes.Buffer
	var exitCode *int
	signaled := false
	err := inst.ExecStream(ctx, client.ExecRequest{
		Command: []string{"sh", "-lc", "trap 'echo got-term; exit 7' TERM; echo signal-ready; while :; do sleep 1; done"},
	}, inputs, func(event client.ExecEvent) error {
		switch event.Kind {
		case "stdout", "stderr":
			writeExecEventOutput(&output, event)
			if !signaled && strings.Contains(output.String(), "signal-ready") {
				signaled = true
				inputs <- client.ExecInput{Kind: "signal", Signal: "TERM"}
				close(inputs)
			}
		case "exit":
			code := event.ExitCode
			exitCode = &code
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stream guest signal exec: %v\noutput:\n%s\nconsole:\n%s", err, output.String(), runtimeConsoleHistory(inst))
	}
	if exitCode == nil {
		t.Fatalf("stream guest signal exec did not report an exit\noutput:\n%s", output.String())
	}
	if *exitCode != 7 {
		t.Fatalf("stream guest signal exec exited with %d, want 7\noutput:\n%s", *exitCode, output.String())
	}
	return output.String()
}

func runRuntimeControl(t *testing.T, inst Instance, req client.ExecRequest, wantExit int) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runtimeExecTimeout())
	defer cancel()
	var output bytes.Buffer
	var exitCode *int
	err := inst.ExecStream(ctx, req, nil, func(event client.ExecEvent) error {
		switch event.Kind {
		case "stdout", "stderr":
			writeExecEventOutput(&output, event)
		case "exit":
			code := event.ExitCode
			exitCode = &code
		}
		return nil
	})
	if err != nil {
		t.Fatalf("run guest control request %q path %q: %v\noutput:\n%s\nconsole:\n%s", req.Kind, req.Path, err, output.String(), runtimeConsoleHistory(inst))
	}
	if exitCode == nil {
		t.Fatalf("guest control request %q path %q did not report an exit\noutput:\n%s", req.Kind, req.Path, output.String())
	}
	if *exitCode != wantExit {
		t.Fatalf("guest control request %q path %q exited with %d, want %d\noutput:\n%s", req.Kind, req.Path, *exitCode, wantExit, output.String())
	}
	return output.Bytes()
}

func addShareWithTimeout(t *testing.T, inst Instance, share client.ShareMount) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runtimeExecTimeout())
	defer cancel()
	if err := inst.AddShare(ctx, share); err != nil {
		t.Fatalf("add runtime share %q: %v", share.Mount, err)
	}
}

func addShareExpectError(t *testing.T, inst Instance, share client.ShareMount) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runtimeExecTimeout())
	defer cancel()
	err := inst.AddShare(ctx, share)
	if err == nil {
		t.Fatalf("add runtime share %q unexpectedly succeeded", share.Mount)
	}
	return err
}

func addImageWithTimeout(t *testing.T, adder runtimeImageAdder, mount string, image *oci.Image) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runtimeExecTimeout())
	defer cancel()
	if err := adder.AddImage(ctx, mount, image); err != nil {
		t.Fatalf("add runtime image at %q: %v", mount, err)
	}
}

func addImageExpectError(t *testing.T, adder runtimeImageAdder, mount string, image *oci.Image) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runtimeExecTimeout())
	defer cancel()
	err := adder.AddImage(ctx, mount, image)
	if err == nil {
		t.Fatalf("add runtime image at %q unexpectedly succeeded", mount)
	}
	return err
}

func flushRuntimeInstance(t *testing.T, inst Instance) {
	t.Helper()
	flusher, ok := inst.(instanceFlushProvider)
	if !ok {
		t.Fatalf("runtime instance does not support flushing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeExecTimeout())
	defer cancel()
	if err := flusher.Flush(ctx); err != nil {
		t.Fatalf("flush guest filesystem state: %v", err)
	}
}

type bootEventRecorder struct {
	mu     sync.Mutex
	events []client.BootEvent
}

func (r *bootEventRecorder) Record(event client.BootEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return nil
}

func (r *bootEventRecorder) RequireKind(t *testing.T, kind string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		for _, event := range r.events {
			if event.Kind == kind {
				r.mu.Unlock()
				return
			}
		}
		events := append([]client.BootEvent(nil), r.events...)
		r.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("boot events did not include kind %q: %+v", kind, events)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeExecEventOutput(output *bytes.Buffer, event client.ExecEvent) {
	if len(event.Data) > 0 {
		output.Write(event.Data)
		return
	}
	output.WriteString(event.Output)
}

func runtimeConsoleHistory(inst Instance) string {
	historyProvider, ok := inst.(consoleHistoryProvider)
	if !ok {
		return ""
	}
	history, _ := historyProvider.ConsoleHistory(context.Background())
	return history
}

func requireRunResponse(t *testing.T, resp client.ExecResponse, err error, wantExit int) {
	t.Helper()
	if err != nil {
		t.Fatalf("run guest command: %v\noutput:\n%s", err, resp.Output)
	}
	if resp.ExitCode != wantExit {
		t.Fatalf("guest command exited with %d, want %d\noutput:\n%s", resp.ExitCode, wantExit, resp.Output)
	}
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustReadHostFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func readImageFile(t *testing.T, root imagefs.Directory, guestPath string) string {
	t.Helper()
	entry, err := imagefs.LookupPath(root, guestPath)
	if err != nil {
		t.Fatalf("lookup snapshot path %s: %v", guestPath, err)
	}
	if entry.File == nil {
		t.Fatalf("snapshot path %s is not a file", guestPath)
	}
	size, _ := entry.File.Stat()
	data, err := entry.File.ReadAt(0, uint32(size))
	if err != nil {
		t.Fatalf("read snapshot path %s: %v", guestPath, err)
	}
	return string(data)
}

func tarPayload(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, contents := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(contents)),
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := io.WriteString(tw, contents); err != nil {
			t.Fatalf("write tar contents %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar payload: %v", err)
	}
	return buf.Bytes()
}

func requireTarFile(t *testing.T, archive []byte, name, want string) {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(archive))
	var names []string
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read tar archive: %v", err)
		}
		names = append(names, header.Name)
		if header.Name != name {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar file %s: %v", name, err)
		}
		if string(data) != want {
			t.Fatalf("tar file %s = %q, want %q", name, string(data), want)
		}
		return
	}
	t.Fatalf("tar archive did not contain %s; entries: %s", name, strings.Join(names, ", "))
}

func requireErrorContains(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", fragment)
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("expected error containing %q, got %v", fragment, err)
	}
}

func requireGuestOutput(t *testing.T, output string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(output, fragment) {
			t.Fatalf("guest output does not contain %q\noutput:\n%s", fragment, output)
		}
	}
}

func runtimeBootContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func runtimeBootRepoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func runtimeBootCacheRoot(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"CCX3_VM_TEST_CACHE_DIR", "CCX3_HVF_TEST_CACHE_DIR"} {
		if dir := strings.TrimSpace(os.Getenv(name)); dir != "" {
			return dir
		}
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil || cacheRoot == "" {
		return filepath.Join(os.TempDir(), "ccx3-runtime-boot-test")
	}
	return filepath.Join(cacheRoot, "ccx3", "runtime-boot-test")
}

func runtimePrepareTimeout() time.Duration {
	return envDuration([]string{"CCX3_VM_TEST_PREPARE_TIMEOUT", "CCX3_HVF_TEST_PREPARE_TIMEOUT"}, 10*time.Minute)
}

func runtimeBootTimeout() time.Duration {
	return envDuration([]string{"CCX3_VM_TEST_BOOT_TIMEOUT", "CCX3_HVF_TEST_BOOT_TIMEOUT"}, 3*time.Minute)
}

func runtimeExecTimeout() time.Duration {
	return envDuration([]string{"CCX3_VM_TEST_EXEC_TIMEOUT", "CCX3_HVF_TEST_EXEC_TIMEOUT"}, time.Minute)
}

func envDuration(names []string, fallback time.Duration) time.Duration {
	for _, name := range names {
		raw := strings.TrimSpace(os.Getenv(name))
		if raw == "" {
			continue
		}
		duration, err := time.ParseDuration(raw)
		if err == nil {
			return duration
		}
		seconds, err := strconv.ParseFloat(raw, 64)
		if err == nil && seconds > 0 {
			return time.Duration(seconds * float64(time.Second))
		}
		fmt.Fprintf(os.Stderr, "invalid %s duration %q; using %s\n", name, raw, fallback)
		return fallback
	}
	return fallback
}
