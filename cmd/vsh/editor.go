package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

var errLineInterrupted = errors.New("line interrupted")

type lineEditor struct {
	in        *os.File
	out       io.Writer
	history   *lineHistory
	completer *vshCompleter
	width     int
	pageSize  int
	prompt    string
	buf       []rune
	cursor    int
	menu      completionMenu
}

type lineHistory struct {
	path  string
	limit int
	items []string
}

type completionMenu struct {
	active     bool
	items      []string
	replaceLen int
	token      string
	selected   int
	page       int
	pageSize   int
}

type editorKey int

const (
	keyRune editorKey = iota
	keyEnter
	keyBackspace
	keyDelete
	keyLeft
	keyRight
	keyUp
	keyDown
	keyHome
	keyEnd
	keyTab
	keyBackTab
	keyEscape
	keyCtrlC
	keyCtrlD
	keyCtrlL
	keyPageUp
	keyPageDown
	keyUnknown
)

type keyEvent struct {
	key editorKey
	r   rune
}

func newLineEditor(in *os.File, out io.Writer, historyPath string, completer *vshCompleter) *lineEditor {
	width := 80
	if file, ok := out.(*os.File); ok {
		if cols, _, err := terminalSize(file); err == nil && cols > 0 {
			width = cols
		}
	}
	return &lineEditor{
		in:        in,
		out:       out,
		history:   loadLineHistory(historyPath, 1000),
		completer: completer,
		width:     width,
		pageSize:  completionPageSize(),
	}
}

func loadLineHistory(path string, limit int) *lineHistory {
	h := &lineHistory{path: path, limit: limit}
	data, err := os.ReadFile(path)
	if err != nil {
		return h
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if shouldSaveHistory(line) {
			h.items = append(h.items, line)
		}
	}
	h.trim()
	return h
}

func (h *lineHistory) add(line string) {
	if h == nil || !shouldSaveHistory(line) {
		return
	}
	h.items = append(h.items, line)
	h.trim()
	if h.path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(h.path), 0o755)
	_ = os.WriteFile(h.path, []byte(strings.Join(h.items, "\n")+"\n"), 0o600)
}

func (h *lineHistory) trim() {
	if h == nil || h.limit <= 0 || len(h.items) <= h.limit {
		return
	}
	h.items = append([]string(nil), h.items[len(h.items)-h.limit:]...)
}

func (e *lineEditor) ReadLine(prompt string) (string, error) {
	restore, err := makeRawTerminal(e.in)
	if err != nil {
		return "", err
	}
	defer restore()

	e.prompt = prompt
	e.buf = nil
	e.cursor = 0
	e.menu = completionMenu{}
	historyPos := len(e.historyItems())
	draft := ""
	e.refresh()
	for {
		ev, err := e.readKey()
		if err != nil {
			restore()
			if errors.Is(err, io.EOF) {
				return "", io.EOF
			}
			return "", err
		}
		switch ev.key {
		case keyRune:
			e.insertRune(ev.r)
			historyPos = len(e.historyItems())
			draft = string(e.buf)
		case keyEnter:
			if e.menu.active {
				e.acceptCompletion()
				break
			}
			line := string(e.buf)
			e.menu = completionMenu{}
			e.refresh()
			fmt.Fprint(e.out, "\r\n")
			restore()
			e.history.add(line)
			return line, nil
		case keyCtrlC:
			e.menu = completionMenu{}
			e.buf = nil
			e.cursor = 0
			e.refresh()
			fmt.Fprint(e.out, "^C\r\n")
			restore()
			return "", errLineInterrupted
		case keyCtrlD:
			if len(e.buf) == 0 {
				e.menu = completionMenu{}
				e.refresh()
				fmt.Fprint(e.out, "\r\n")
				restore()
				return "", io.EOF
			}
			e.deleteRight()
		case keyCtrlL:
			fmt.Fprint(e.out, "\x1b[H\x1b[2J")
			e.refresh()
		case keyBackspace:
			e.deleteLeft()
			historyPos = len(e.historyItems())
			draft = string(e.buf)
		case keyDelete:
			e.deleteRight()
			historyPos = len(e.historyItems())
			draft = string(e.buf)
		case keyLeft:
			e.menu.active = false
			if e.cursor > 0 {
				e.cursor--
			}
		case keyRight:
			e.menu.active = false
			if e.cursor < len(e.buf) {
				e.cursor++
			}
		case keyHome:
			e.menu.active = false
			e.cursor = 0
		case keyEnd:
			e.menu.active = false
			e.cursor = len(e.buf)
		case keyUp:
			if e.menu.active {
				e.moveCompletion(-e.menuColumns())
				break
			}
			historyPos, draft = e.historyMove(historyPos, draft, -1)
		case keyDown:
			if e.menu.active {
				e.moveCompletion(e.menuColumns())
				break
			}
			historyPos, draft = e.historyMove(historyPos, draft, 1)
		case keyTab:
			if e.menu.active {
				e.moveCompletion(1)
			} else {
				e.startCompletion()
			}
		case keyBackTab:
			if e.menu.active {
				e.moveCompletion(-1)
			}
		case keyPageUp:
			if e.menu.active {
				e.pageCompletion(-1)
			}
		case keyPageDown:
			if e.menu.active {
				e.pageCompletion(1)
			}
		case keyEscape:
			e.menu.active = false
		case keyUnknown:
		}
		e.refresh()
	}
}

func (e *lineEditor) historyItems() []string {
	if e.history == nil {
		return nil
	}
	return e.history.items
}

func (e *lineEditor) historyMove(pos int, draft string, delta int) (int, string) {
	items := e.historyItems()
	if len(items) == 0 {
		return pos, draft
	}
	if pos == len(items) {
		draft = string(e.buf)
	}
	pos += delta
	if pos < 0 {
		pos = 0
	}
	if pos > len(items) {
		pos = len(items)
	}
	if pos == len(items) {
		e.buf = []rune(draft)
	} else {
		e.buf = []rune(items[pos])
	}
	e.cursor = len(e.buf)
	e.menu.active = false
	return pos, draft
}

func (e *lineEditor) insertRune(r rune) {
	e.menu.active = false
	e.buf = append(e.buf, 0)
	copy(e.buf[e.cursor+1:], e.buf[e.cursor:])
	e.buf[e.cursor] = r
	e.cursor++
}

func (e *lineEditor) deleteLeft() {
	e.menu.active = false
	if e.cursor == 0 {
		return
	}
	copy(e.buf[e.cursor-1:], e.buf[e.cursor:])
	e.buf = e.buf[:len(e.buf)-1]
	e.cursor--
}

func (e *lineEditor) deleteRight() {
	e.menu.active = false
	if e.cursor >= len(e.buf) {
		return
	}
	copy(e.buf[e.cursor:], e.buf[e.cursor+1:])
	e.buf = e.buf[:len(e.buf)-1]
}

func (e *lineEditor) startCompletion() {
	if e.completer == nil {
		return
	}
	items, replaceLen := e.completer.Complete(e.buf, e.cursor)
	if len(items) == 0 {
		e.bell()
		return
	}
	if len(items) == 1 {
		e.insertCompletion(items[0], replaceLen)
		return
	}
	if common := commonCompletionPrefix(items); common != "" {
		e.insertCompletion(common, replaceLen)
		return
	}
	tokenStart := e.cursor - replaceLen
	if tokenStart < 0 {
		tokenStart = 0
	}
	e.menu = completionMenu{
		active:     true,
		items:      items,
		replaceLen: replaceLen,
		token:      string(e.buf[tokenStart:e.cursor]),
		selected:   0,
		pageSize:   e.pageSize,
	}
}

func (e *lineEditor) acceptCompletion() {
	if !e.menu.active || len(e.menu.items) == 0 {
		return
	}
	selected := e.menu.selected
	if selected < 0 || selected >= len(e.menu.items) {
		selected = 0
	}
	replaceLen := e.menu.replaceLen
	item := e.menu.items[selected]
	e.menu.active = false
	e.insertCompletion(item, replaceLen)
}

func (e *lineEditor) insertCompletion(value string, replaceLen int) {
	_ = replaceLen
	replacement := []rune(value)
	next := make([]rune, 0, len(e.buf)+len(replacement))
	next = append(next, e.buf[:e.cursor]...)
	next = append(next, replacement...)
	next = append(next, e.buf[e.cursor:]...)
	e.buf = next
	e.cursor += len(replacement)
}

func (e *lineEditor) moveCompletion(delta int) {
	if !e.menu.active || len(e.menu.items) == 0 {
		return
	}
	e.menu.selected += delta
	if e.menu.selected < 0 {
		e.menu.selected = len(e.menu.items) - 1
	}
	if e.menu.selected >= len(e.menu.items) {
		e.menu.selected = 0
	}
	e.menu.page = e.menu.selected / e.effectivePageSize()
}

func (e *lineEditor) pageCompletion(delta int) {
	if !e.menu.active || len(e.menu.items) == 0 {
		return
	}
	pageSize := e.effectivePageSize()
	pages := (len(e.menu.items) + pageSize - 1) / pageSize
	e.menu.page += delta
	if e.menu.page < 0 {
		e.menu.page = pages - 1
	}
	if e.menu.page >= pages {
		e.menu.page = 0
	}
	e.menu.selected = e.menu.page * pageSize
	if e.menu.selected >= len(e.menu.items) {
		e.menu.selected = len(e.menu.items) - 1
	}
}

func (e *lineEditor) effectivePageSize() int {
	if e.menu.pageSize > 0 {
		return e.menu.pageSize
	}
	if e.pageSize > 0 {
		return e.pageSize
	}
	return defaultCompletionPageSize
}

func (e *lineEditor) menuColumns() int {
	page := e.menuPageItems()
	if len(page) == 0 {
		return 1
	}
	width := maxCompletionDisplayWidth(page, e.menu.token) + 2
	if width < 8 {
		width = 8
	}
	cols := e.width / width
	if cols < 1 {
		return 1
	}
	return cols
}

func (e *lineEditor) menuPageItems() []string {
	if !e.menu.active {
		return nil
	}
	pageSize := e.effectivePageSize()
	start := e.menu.page * pageSize
	if start >= len(e.menu.items) {
		start = 0
		e.menu.page = 0
	}
	end := start + pageSize
	if end > len(e.menu.items) {
		end = len(e.menu.items)
	}
	return e.menu.items[start:end]
}

func (e *lineEditor) refresh() {
	before := string(e.buf[:e.cursor])
	after := string(e.buf[e.cursor:])
	fmt.Fprint(e.out, "\r\x1b[J")
	fmt.Fprint(e.out, e.prompt)
	fmt.Fprint(e.out, before)
	fmt.Fprint(e.out, "\x1b7")
	fmt.Fprint(e.out, after)
	if e.menu.active {
		e.renderMenu()
	}
	fmt.Fprint(e.out, "\x1b8")
}

func (e *lineEditor) renderMenu() {
	page := e.menuPageItems()
	if len(page) == 0 {
		return
	}
	cols := e.menuColumns()
	width := maxCompletionDisplayWidth(page, e.menu.token) + 2
	if width < 8 {
		width = 8
	}
	start := e.menu.page * e.effectivePageSize()
	for i, item := range page {
		if i%cols == 0 {
			fmt.Fprint(e.out, "\r\n")
		}
		idx := start + i
		text := e.completionDisplayText(item)
		if idx == e.menu.selected {
			fmt.Fprint(e.out, "\x1b[30;47m")
		}
		fmt.Fprint(e.out, padRight(text, width))
		if idx == e.menu.selected {
			fmt.Fprint(e.out, colorReset)
		}
	}
	total := len(e.menu.items)
	pageSize := e.effectivePageSize()
	if total > pageSize {
		pages := (total + pageSize - 1) / pageSize
		fmt.Fprintf(e.out, "\r\n%s%d-%d of %d, page %d/%d%s",
			colorYellow,
			start+1,
			start+len(page),
			total,
			e.menu.page+1,
			pages,
			colorReset,
		)
	}
}

func (e *lineEditor) completionDisplayText(item string) string {
	if e.menu.token == "" {
		return item
	}
	return e.menu.token + item
}

func (e *lineEditor) bell() {
	fmt.Fprint(e.out, "\a")
}

func (e *lineEditor) readKey() (keyEvent, error) {
	var buf [1]byte
	for {
		n, err := e.in.Read(buf[:])
		if n > 0 {
			return e.decodeKey(buf[0])
		}
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			return keyEvent{}, err
		}
	}
}

func (e *lineEditor) decodeKey(b byte) (keyEvent, error) {
	switch b {
	case '\r', '\n':
		return keyEvent{key: keyEnter}, nil
	case '\t':
		return keyEvent{key: keyTab}, nil
	case 0x7f, 0x08:
		return keyEvent{key: keyBackspace}, nil
	case 0x03:
		return keyEvent{key: keyCtrlC}, nil
	case 0x04:
		return keyEvent{key: keyCtrlD}, nil
	case 0x0c:
		return keyEvent{key: keyCtrlL}, nil
	case 0x1b:
		return e.readEscape()
	default:
		if b < 0x20 {
			return keyEvent{key: keyUnknown}, nil
		}
		return e.readRune(b)
	}
}

func (e *lineEditor) readRune(first byte) (keyEvent, error) {
	if first < utf8.RuneSelf {
		return keyEvent{key: keyRune, r: rune(first)}, nil
	}
	buf := []byte{first}
	for !utf8.FullRune(buf) && len(buf) < utf8.UTFMax {
		b, err := e.readByteBlocking()
		if err != nil {
			return keyEvent{}, err
		}
		buf = append(buf, b)
	}
	r, _ := utf8.DecodeRune(buf)
	return keyEvent{key: keyRune, r: r}, nil
}

func (e *lineEditor) readEscape() (keyEvent, error) {
	b, err := e.readByteWithTimeout(15 * time.Millisecond)
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return keyEvent{key: keyEscape}, nil
		}
		return keyEvent{}, err
	}
	if b != '[' && b != 'O' {
		return keyEvent{key: keyEscape}, nil
	}
	next, err := e.readByteBlocking()
	if err != nil {
		return keyEvent{}, err
	}
	switch next {
	case 'A':
		return keyEvent{key: keyUp}, nil
	case 'B':
		return keyEvent{key: keyDown}, nil
	case 'C':
		return keyEvent{key: keyRight}, nil
	case 'D':
		return keyEvent{key: keyLeft}, nil
	case 'H':
		return keyEvent{key: keyHome}, nil
	case 'F':
		return keyEvent{key: keyEnd}, nil
	case 'Z':
		return keyEvent{key: keyBackTab}, nil
	case '1', '3', '4', '5', '6', '7', '8':
		term, err := e.readByteBlocking()
		if err != nil {
			return keyEvent{}, err
		}
		if term != '~' {
			return keyEvent{key: keyEscape}, nil
		}
		switch next {
		case '1', '7':
			return keyEvent{key: keyHome}, nil
		case '3':
			return keyEvent{key: keyDelete}, nil
		case '4', '8':
			return keyEvent{key: keyEnd}, nil
		case '5':
			return keyEvent{key: keyPageUp}, nil
		case '6':
			return keyEvent{key: keyPageDown}, nil
		}
	}
	return keyEvent{key: keyEscape}, nil
}

func (e *lineEditor) readByteBlocking() (byte, error) {
	for {
		b, err := e.readByteWithTimeout(0)
		if err == nil {
			return b, nil
		}
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, os.ErrDeadlineExceeded) {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		return 0, err
	}
}

func (e *lineEditor) readByteWithTimeout(timeout time.Duration) (byte, error) {
	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	var buf [1]byte
	for {
		n, err := e.in.Read(buf[:])
		if n > 0 {
			return buf[0], nil
		}
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				if !deadline.IsZero() && time.Now().After(deadline) {
					return 0, os.ErrDeadlineExceeded
				}
				time.Sleep(2 * time.Millisecond)
				continue
			}
			return 0, err
		}
	}
}

func commonCompletionPrefix(items []string) string {
	if len(items) < 2 {
		return ""
	}
	prefix := []rune(items[0])
	for _, item := range items[1:] {
		runes := []rune(item)
		i := 0
		for i < len(prefix) && i < len(runes) && prefix[i] == runes[i] {
			i++
		}
		prefix = prefix[:i]
		if len(prefix) == 0 {
			return ""
		}
	}
	return string(prefix)
}

func maxCompletionDisplayWidth(items []string, token string) int {
	max := 0
	for _, item := range items {
		if n := len([]rune(token + item)); n > max {
			max = n
		}
	}
	return max
}

func padRight(value string, width int) string {
	padding := width - len([]rune(value))
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}
