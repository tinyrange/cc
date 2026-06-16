//go:build freebsd

package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const (
	readyMarker = "__CCX3_READY__"
	beginMarker = "__CCX3_BEGIN__:"
	outMarker   = "__CCX3_OUT__:"
	errMarker   = "__CCX3_ERR__:"
	ctlMarker   = "__CCX3_CTL__:"
	exitMarker  = "__CCX3_EXIT__:"
)

type request struct {
	Kind      string   `json:"kind,omitempty"`
	ID        string   `json:"id"`
	Command   []string `json:"command,omitempty"`
	Env       []string `json:"env,omitempty"`
	RootDir   string   `json:"root_dir,omitempty"`
	Path      string   `json:"path,omitempty"`
	Directory bool     `json:"directory,omitempty"`
	WorkDir   string   `json:"workdir,omitempty"`
	Stdin     []byte   `json:"stdin,omitempty"`
	TTY       bool     `json:"tty,omitempty"`
	ControlFD bool     `json:"control_fd,omitempty"`
	Signal    string   `json:"signal,omitempty"`
	Cols      int      `json:"cols,omitempty"`
	Rows      int      `json:"rows,omitempty"`
}

type managedExec struct {
	mu      sync.Mutex
	stdin   io.WriteCloser
	process *os.Process
	pty     *os.File
}

func (m *managedExec) write(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stdin == nil {
		return fmt.Errorf("stdin is closed")
	}
	_, err := m.stdin.Write(data)
	return err
}

func (m *managedExec) close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stdin == nil {
		return nil
	}
	err := m.stdin.Close()
	m.stdin = nil
	return err
}

func (m *managedExec) setProcess(p *os.Process) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.process = p
}

func (m *managedExec) setPTY(pty *os.File) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pty = pty
}

func (m *managedExec) resize(cols, rows int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pty == nil {
		return fmt.Errorf("exec has no tty")
	}
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("invalid tty size %dx%d", cols, rows)
	}
	return pty.Setsize(m.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (m *managedExec) signal(name string) error {
	sig, err := parseSignal(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.process == nil {
		return fmt.Errorf("process is not started")
	}
	if m.process.Pid > 0 {
		if err := syscall.Kill(-m.process.Pid, sig); err == nil || errors.Is(err, syscall.ESRCH) {
			return nil
		}
	}
	return m.process.Signal(sig)
}

func main() {
	if err := run(); err != nil {
		writeConsole("ccx3-freebsd-init: " + err.Error() + "\n")
		for {
			time.Sleep(time.Hour)
		}
	}
}

func run() error {
	_ = os.Chdir("/")
	conn, err := connectControl()
	if err != nil {
		return err
	}
	defer conn.Close()
	writeLine(conn, readyMarker)
	return commandLoop(conn)
}

func connectControl() (net.Conn, error) {
	var last error
	for i := 0; i < 80; i++ {
		conn, err := net.DialTimeout("tcp4", "10.42.0.1:10777", time.Second)
		if err == nil {
			return conn, nil
		}
		last = err
		time.Sleep(250 * time.Millisecond)
	}
	return nil, fmt.Errorf("connect control: %w", last)
}

func commandLoop(control net.Conn) error {
	reader := bufio.NewReader(control)
	active := map[string]*managedExec{}
	pending := map[string]request{}
	var mu sync.Mutex
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("read request: %w", err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			writeConsole("ccx3-freebsd-init: decode request: " + err.Error() + "\n")
			continue
		}
		if req.Kind == "" {
			req.Kind = "exec"
		}
		switch req.Kind {
		case "exec":
			if req.ID == "" || len(req.Command) == 0 {
				continue
			}
			if len(req.Stdin) != 0 {
				startExecWithReader(control, req, io.NopCloser(bytes.NewReader(req.Stdin)), nil, func() {})
				continue
			}
			if req.TTY || req.ControlFD {
				stdinR, stdinW := io.Pipe()
				managed := &managedExec{stdin: stdinW}
				mu.Lock()
				active[req.ID] = managed
				mu.Unlock()
				go runExec(control, req, stdinR, managed, func() {
					_ = managed.close()
					mu.Lock()
					delete(active, req.ID)
					mu.Unlock()
				})
				continue
			}
			mu.Lock()
			pending[req.ID] = req
			mu.Unlock()
		case "sync":
			go runSync(control, req.ID)
		case "fs_mkdir":
			go runMkdir(control, req)
		case "fs_write":
			go runWrite(control, req)
		case "fs_extract":
			if len(req.Stdin) > 0 {
				go runExtract(control, req, io.NopCloser(bytes.NewReader(req.Stdin)), func() {})
				continue
			}
			stdinR, stdinW := io.Pipe()
			managed := &managedExec{stdin: stdinW}
			mu.Lock()
			active[req.ID] = managed
			mu.Unlock()
			go runExtract(control, req, stdinR, func() {
				_ = managed.close()
				mu.Lock()
				delete(active, req.ID)
				mu.Unlock()
			})
		case "fs_archive":
			go runArchive(control, req)
		case "stdin":
			mu.Lock()
			pendingReq, pendingOK := pending[req.ID]
			if pendingOK {
				delete(pending, req.ID)
			}
			mu.Unlock()
			if pendingOK {
				stdinR, stdinW := io.Pipe()
				managed := &managedExec{stdin: stdinW}
				mu.Lock()
				active[req.ID] = managed
				mu.Unlock()
				go runExec(control, pendingReq, stdinR, managed, func() {
					_ = managed.close()
					mu.Lock()
					delete(active, req.ID)
					mu.Unlock()
				})
				_ = managed.write(req.Stdin)
				continue
			}
			mu.Lock()
			managed := active[req.ID]
			mu.Unlock()
			if managed != nil {
				_ = managed.write(req.Stdin)
			}
		case "stdin_close":
			mu.Lock()
			pendingReq, pendingOK := pending[req.ID]
			if pendingOK {
				delete(pending, req.ID)
			}
			mu.Unlock()
			if pendingOK {
				startExecWithReader(control, pendingReq, io.NopCloser(bytes.NewReader(nil)), nil, func() {})
				continue
			}
			mu.Lock()
			managed := active[req.ID]
			mu.Unlock()
			if managed != nil {
				_ = managed.close()
			}
		case "signal":
			mu.Lock()
			managed := active[req.ID]
			mu.Unlock()
			if managed != nil {
				_ = managed.signal(req.Signal)
			}
		case "resize":
			mu.Lock()
			managed := active[req.ID]
			mu.Unlock()
			if managed != nil {
				_ = managed.resize(req.Cols, req.Rows)
			}
		}
	}
}

func startExecWithReader(control net.Conn, req request, stdin io.ReadCloser, managed *managedExec, cleanup func()) {
	go runExec(control, req, stdin, managed, cleanup)
}

func openPTY(cols, rows int) (*os.File, *os.File, error) {
	master, slave, err := pty.Open()
	if err != nil {
		return nil, nil, err
	}
	if cols > 0 && rows > 0 {
		if err := pty.Setsize(master, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
			_ = master.Close()
			_ = slave.Close()
			return nil, nil, fmt.Errorf("set initial winsize: %w", err)
		}
	}
	return master, slave, nil
}

func runExec(control io.Writer, req request, stdin io.ReadCloser, managed *managedExec, cleanup func()) {
	defer cleanup()
	writeLine(control, beginMarker+req.ID)
	cmd := exec.Command(req.Command[0], req.Command[1:]...)
	if req.WorkDir != "" {
		cmd.Dir = rootPath(req.RootDir, req.WorkDir)
	}
	if len(req.Env) != 0 {
		cmd.Env = req.Env
	}
	var controlR, controlW *os.File
	if req.ControlFD {
		var err error
		controlR, controlW, err = os.Pipe()
		if err != nil {
			if managed != nil {
				_ = managed.close()
			}
			writeErr(control, req.ID, fmt.Errorf("open control fd: %w", err))
			writeLine(control, exitMarker+req.ID+":126")
			return
		}
		cmd.ExtraFiles = append(cmd.ExtraFiles, controlW)
	}
	var wg sync.WaitGroup
	var stdoutW, stderrW *io.PipeWriter
	var ptyMaster, ptySlave *os.File
	if req.TTY {
		var err error
		ptyMaster, ptySlave, err = openPTY(req.Cols, req.Rows)
		if err != nil {
			if managed != nil {
				_ = managed.close()
			}
			if controlR != nil {
				_ = controlR.Close()
			}
			if controlW != nil {
				_ = controlW.Close()
			}
			writeErr(control, req.ID, fmt.Errorf("open pty: %w", err))
			writeLine(control, exitMarker+req.ID+":126")
			return
		}
		if managed != nil {
			managed.setPTY(ptyMaster)
		}
		cmd.Stdin = ptySlave
		cmd.Stdout = ptySlave
		cmd.Stderr = ptySlave
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid:  true,
			Setctty: true,
			Ctty:    0,
		}
		wg.Add(1)
		go copyMarked(&wg, control, outMarker, req.ID, ptyMaster)
		if stdin != nil {
			wg.Add(1)
			go copyPTYStdin(&wg, stdin, ptyMaster)
		}
	} else {
		cmd.Stdin = stdin
		stdoutR, stdoutPipeW := io.Pipe()
		stderrR, stderrPipeW := io.Pipe()
		stdoutW = stdoutPipeW
		stderrW = stderrPipeW
		cmd.Stdout = stdoutW
		cmd.Stderr = stderrW
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		wg.Add(2)
		go copyMarked(&wg, control, outMarker, req.ID, stdoutR)
		go copyMarked(&wg, control, errMarker, req.ID, stderrR)
	}
	if controlR != nil {
		wg.Add(1)
		go copyMarked(&wg, control, ctlMarker, req.ID, controlR)
	}
	if err := cmd.Start(); err != nil {
		if managed != nil {
			_ = managed.close()
		}
		if controlR != nil {
			_ = controlR.Close()
		}
		if controlW != nil {
			_ = controlW.Close()
		}
		if stdoutW != nil {
			_ = stdoutW.Close()
		}
		if stderrW != nil {
			_ = stderrW.Close()
		}
		if ptyMaster != nil {
			_ = ptyMaster.Close()
		}
		if ptySlave != nil {
			_ = ptySlave.Close()
		}
		wg.Wait()
		writeErr(control, req.ID, err)
		writeLine(control, exitMarker+req.ID+":126")
		return
	}
	if controlW != nil {
		_ = controlW.Close()
	}
	if managed != nil {
		managed.setProcess(cmd.Process)
	}
	waitErr := cmd.Wait()
	_ = stdin.Close()
	if stdoutW != nil {
		_ = stdoutW.Close()
	}
	if stderrW != nil {
		_ = stderrW.Close()
	}
	if ptySlave != nil {
		_ = ptySlave.Close()
	}
	if ptyMaster != nil {
		_ = ptyMaster.Close()
	}
	wg.Wait()
	if controlR != nil {
		_ = controlR.Close()
	}
	code := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			writeErr(control, req.ID, waitErr)
			code = 126
		}
	}
	writeLine(control, exitMarker+req.ID+":"+itoa(code))
}

func runSync(control io.Writer, id string) {
	writeLine(control, beginMarker+id)
	syscall.Sync()
	writeLine(control, exitMarker+id+":0")
}

func runMkdir(control io.Writer, req request) {
	writeLine(control, beginMarker+req.ID)
	code := 0
	target := rootPath(req.RootDir, firstNonEmpty(req.Path, "."))
	if err := os.MkdirAll(target, 0o755); err != nil {
		writeErr(control, req.ID, err)
		code = 1
	}
	writeLine(control, exitMarker+req.ID+":"+itoa(code))
}

func runWrite(control io.Writer, req request) {
	writeLine(control, beginMarker+req.ID)
	code := 0
	if strings.TrimSpace(req.Path) == "" {
		writeErr(control, req.ID, fmt.Errorf("destination path is required"))
		code = 1
	} else {
		target := rootPath(req.RootDir, req.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			writeErr(control, req.ID, err)
			code = 1
		} else if err := os.WriteFile(target, req.Stdin, 0o644); err != nil {
			writeErr(control, req.ID, err)
			code = 1
		}
	}
	writeLine(control, exitMarker+req.ID+":"+itoa(code))
}

func runExtract(control io.Writer, req request, r io.ReadCloser, cleanup func()) {
	defer cleanup()
	defer r.Close()
	writeLine(control, beginMarker+req.ID)
	code := 0
	if err := extractTarToPath(r, req.RootDir, req.Path, req.Directory); err != nil {
		writeErr(control, req.ID, err)
		code = 1
	}
	writeLine(control, exitMarker+req.ID+":"+itoa(code))
}

func runArchive(control io.Writer, req request) {
	writeLine(control, beginMarker+req.ID)
	code := 0
	if err := archivePath(control, req.ID, req.RootDir, req.Path); err != nil {
		writeErr(control, req.ID, err)
		code = 1
	}
	writeLine(control, exitMarker+req.ID+":"+itoa(code))
}

func archivePath(control io.Writer, id, rootDir, src string) error {
	if strings.TrimSpace(src) == "" {
		return fmt.Errorf("source path is required")
	}
	src = rootPath(rootDir, src)
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	go func() {
		err := writePathTar(pw, src, filepath.Base(src), info)
		_ = pw.CloseWithError(err)
	}()
	var buf [32768]byte
	for {
		n, err := pr.Read(buf[:])
		if n > 0 {
			writeBytes(control, outMarker, id, buf[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func writePathTar(w io.Writer, src, rootName string, rootInfo os.FileInfo) error {
	tw := tar.NewWriter(w)
	defer tw.Close()
	return filepath.WalkDir(src, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(src), filePath)
		if err != nil {
			return err
		}
		if rel == "." {
			rel = rootName
			info = rootInfo
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func extractTarToPath(r io.Reader, rootDir, dst string, dstDir bool) error {
	if strings.TrimSpace(dst) == "" {
		return fmt.Errorf("destination path is required")
	}
	dst = rootPath(rootDir, dst)
	if info, err := os.Stat(dst); err == nil && info.IsDir() {
		dstDir = true
	}
	tr := tar.NewReader(r)
	sawEntry := false
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			if !sawEntry {
				return fmt.Errorf("archive is empty")
			}
			return nil
		}
		if err != nil {
			return err
		}
		sawEntry = true
		target, err := tarTarget(dst, dstDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode).Perm()); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode).Perm())
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, tr)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
}

func tarTarget(dst string, dstDir bool, name string) (string, error) {
	cleanName := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(name, "/")))
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe tar path %q", name)
	}
	if dstDir {
		return filepath.Join(dst, cleanName), nil
	}
	parts := strings.SplitN(cleanName, string(filepath.Separator), 2)
	if len(parts) == 1 {
		return dst, nil
	}
	return filepath.Join(dst, parts[1]), nil
}

func copyMarked(wg *sync.WaitGroup, control io.Writer, marker, id string, r io.Reader) {
	defer wg.Done()
	var buf [4096]byte
	for {
		n, err := r.Read(buf[:])
		if n > 0 {
			writeBytes(control, marker, id, buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func copyPTYStdin(wg *sync.WaitGroup, stdin io.ReadCloser, ptyMaster *os.File) {
	defer wg.Done()
	defer stdin.Close()
	var buf [4096]byte
	for {
		n, err := stdin.Read(buf[:])
		if n > 0 {
			if _, writeErr := ptyMaster.Write(buf[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				_, _ = ptyMaster.Write([]byte{4})
			}
			return
		}
	}
}

func writeErr(control io.Writer, id string, err error) {
	writeBytes(control, errMarker, id, []byte("ccx3-freebsd-init: "+err.Error()+"\n"))
}

func writeBytes(control io.Writer, marker, id string, data []byte) {
	writeLine(control, marker+id+":"+base64.StdEncoding.EncodeToString(data))
}

var protocolMu sync.Mutex

func writeLine(w io.Writer, line string) {
	protocolMu.Lock()
	defer protocolMu.Unlock()
	_, _ = io.WriteString(w, line+"\n")
}

func writeConsole(s string) {
	if console, err := os.OpenFile("/dev/console", os.O_RDWR, 0); err == nil {
		defer console.Close()
		_, _ = console.WriteString(s)
		return
	}
	_, _ = os.Stderr.WriteString(s)
}

func rootPath(rootDir, name string) string {
	cleanRoot := filepath.Clean("/" + strings.TrimPrefix(strings.TrimSpace(rootDir), "/"))
	if cleanRoot == "/" {
		cleanRoot = ""
	}
	cleanName := filepath.Clean("/" + strings.TrimPrefix(strings.TrimSpace(name), "/"))
	return filepath.Join(cleanRoot, cleanName)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseSignal(name string) (syscall.Signal, error) {
	switch strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(name), "SIG")) {
	case "", "TERM":
		return syscall.SIGTERM, nil
	case "KILL":
		return syscall.SIGKILL, nil
	case "INT":
		return syscall.SIGINT, nil
	case "HUP":
		return syscall.SIGHUP, nil
	default:
		return 0, fmt.Errorf("unsupported signal %q", name)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
