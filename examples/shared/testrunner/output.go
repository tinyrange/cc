package testrunner

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"

	// Cursor control
	clearLine     = "\033[2K"
	clearToEnd    = "\033[K"
	cursorToStart = "\r"
	hideCursor    = "\033[?25l"
	showCursor    = "\033[?25h"
)

// Spinner frames for progress animation
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Output handles terminal output with optional rich formatting.
type Output struct {
	isTTY       bool
	mu          sync.Mutex
	spinnerStop chan struct{}
	spinnerDone chan struct{}
}

// NewOutput creates a new Output instance.
func NewOutput() *Output {
	return &Output{
		isTTY: false, // Disable fancy terminal output to avoid hangs
	}
}

// IsTTY returns whether the output is a terminal.
func (o *Output) IsTTY() bool {
	return o.isTTY
}

// color wraps text in ANSI color codes if TTY.
func (o *Output) color(code, text string) string {
	if !o.isTTY {
		return text
	}
	return code + text + colorReset
}

func padCenter(text string, width int) string {
	if len(text) >= width {
		return text
	}
	return strings.Repeat(" ", (width-len(text))/2) + text + strings.Repeat(" ", width-len(text)-(width-len(text))/2)
}

// PrintBanner prints a styled banner at the start of the test run.
func (o *Output) PrintBanner(exampleCount, testCount int) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.isTTY {
		fmt.Println()
		fmt.Println(o.color(colorCyan+colorBold, "╭"+strings.Repeat("─", 40)+"╮"))
		fmt.Println(o.color(colorCyan+colorBold, "│") + o.color(colorBold, padCenter("crumblecracker test runner", 40)) + o.color(colorCyan+colorBold, "│"))
		fmt.Println(o.color(colorCyan+colorBold, "╰"+strings.Repeat("─", 40)+"╯"))
		fmt.Printf("  %s %d examples, %d tests\n",
			o.color(colorDim, "Running"),
			exampleCount,
			testCount)
		fmt.Println()
	} else {
		fmt.Println("=== TEST RUNNER ===")
		fmt.Printf("Running %d examples, %d tests\n\n", exampleCount, testCount)
	}
}

// PrintExampleHeader prints the header for an example.
func (o *Output) PrintExampleHeader(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.isTTY {
		fmt.Printf("%s %s\n", o.color(colorBlue+colorBold, "▶"), o.color(colorBold, name))
	} else {
		fmt.Printf("=== %s ===\n", name)
	}
}

// PrintExampleError prints an error for an example.
func (o *Output) PrintExampleError(err string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.isTTY {
		fmt.Printf("  %s %s\n", o.color(colorRed+colorBold, "✗"), o.color(colorRed, err))
	} else {
		fmt.Printf("    ERROR: %s\n", err)
	}
}

// StartTestRun shows the currently running test with a spinner and elapsed time.
func (o *Output) StartTestRun(name string) {
	if !o.isTTY {
		return
	}

	o.mu.Lock()
	if o.spinnerStop != nil {
		o.mu.Unlock()
		return // Spinner already running
	}
	o.spinnerStop = make(chan struct{})
	o.spinnerDone = make(chan struct{})
	o.mu.Unlock()

	go func() {
		defer close(o.spinnerDone)
		frame := 0
		start := time.Now()
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		fmt.Print(hideCursor)

		for {
			select {
			case <-o.spinnerStop:
				// Clear the spinner line
				fmt.Print(cursorToStart + clearLine + showCursor)
				return
			case <-ticker.C:
				elapsed := time.Since(start).Round(100 * time.Millisecond)
				o.mu.Lock()
				fmt.Printf("%s  %s %s %s%s",
					cursorToStart,
					o.color(colorCyan, spinnerFrames[frame]),
					o.color(colorDim, name),
					o.color(colorDim, fmt.Sprintf("(%s)", elapsed)),
					clearToEnd)
				o.mu.Unlock()
				frame = (frame + 1) % len(spinnerFrames)
			}
		}
	}()
}

// StopTestRun stops the test spinner.
func (o *Output) StopTestRun() {
	o.StopSpinner()
}

// PrintTestPass prints a passing test result.
func (o *Output) PrintTestPass(name string, duration time.Duration) {
	o.StopTestRun() // Clear spinner if running

	o.mu.Lock()
	defer o.mu.Unlock()

	if o.isTTY {
		fmt.Printf("  %s %s %s\n",
			o.color(colorGreen, "✓"),
			name,
			o.color(colorDim, fmt.Sprintf("(%s)", duration.Round(time.Millisecond))))
	} else {
		fmt.Printf("    PASS  %s\n", name)
	}
}

// PrintTestFail prints a failing test result with a retry command.
func (o *Output) PrintTestFail(name string, errMsg string, dir string, details interface{}) {
	o.StopTestRun() // Clear spinner if running

	o.mu.Lock()
	defer o.mu.Unlock()

	retryCmd := fmt.Sprintf("./tools/build.go -example-test %s", dir)

	if o.isTTY {
		fmt.Printf("  %s %s\n", o.color(colorRed+colorBold, "✗"), o.color(colorRed, name))
		// Indent error message - split on newlines now
		for _, line := range strings.Split(errMsg, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				fmt.Printf("    %s %s\n", o.color(colorDim, "→"), o.color(colorYellow, line))
			}
		}
		// Print details if available
		o.printDetailsLocked(details)
		// Print retry command
		fmt.Printf("    %s %s\n", o.color(colorDim, "retry:"), o.color(colorCyan, retryCmd))
	} else {
		fmt.Printf("    FAIL  %s:\n", name)
		for _, line := range strings.Split(errMsg, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				fmt.Printf("      %s\n", line)
			}
		}
		o.printDetailsPlainLocked(details)
		fmt.Printf("    retry: %s\n", retryCmd)
	}
}

// printDetailsLocked prints test details with TTY formatting.
// Must be called with o.mu held.
func (o *Output) printDetailsLocked(details interface{}) {
	if details == nil {
		return
	}

	d := extractDetails(details)
	if d == nil {
		return
	}

	// For CLI tests, show args, stderr (first - more important), and stdout
	isCLI := len(d.Args) > 0 || d.ExitCode != 0 || d.Stdout != "" || d.Stderr != ""
	if len(d.Args) > 0 {
		fmt.Printf("    %s %s\n", o.color(colorDim, "args:"), o.color(colorCyan, strings.Join(d.Args, " ")))
	}
	// Always show stderr for CLI tests - it's usually the most important for debugging
	if isCLI {
		if d.Stderr != "" {
			fmt.Printf("    %s\n", o.color(colorDim, "stderr:"))
			o.printIndentedOutput(d.Stderr, 500)
		} else {
			fmt.Printf("    %s %s\n", o.color(colorDim, "stderr:"), o.color(colorDim, "(empty)"))
		}
	}
	if d.Stdout != "" {
		fmt.Printf("    %s\n", o.color(colorDim, "stdout:"))
		o.printIndentedOutput(d.Stdout, 500)
	}

	// For HTTP tests, show request info and response body
	if d.Method != "" {
		fmt.Printf("    %s %s %s\n", o.color(colorDim, "request:"), o.color(colorCyan, d.Method), o.color(colorCyan, d.Path))
	}
	if d.StatusCode != 0 {
		fmt.Printf("    %s %d\n", o.color(colorDim, "status:"), d.StatusCode)
	}
	if d.Body != "" && d.Method != "" {
		fmt.Printf("    %s\n", o.color(colorDim, "response body:"))
		o.printIndentedOutput(d.Body, 500)
	}
}

// printDetailsPlainLocked prints test details without TTY formatting.
// Must be called with o.mu held.
func (o *Output) printDetailsPlainLocked(details interface{}) {
	if details == nil {
		return
	}

	d := extractDetails(details)
	if d == nil {
		return
	}

	// For CLI tests - show stderr first (more important for debugging)
	isCLI := len(d.Args) > 0 || d.ExitCode != 0 || d.Stdout != "" || d.Stderr != ""
	if len(d.Args) > 0 {
		fmt.Printf("      args: %s\n", strings.Join(d.Args, " "))
	}
	// Always show stderr for CLI tests
	if isCLI {
		if d.Stderr != "" {
			fmt.Printf("      stderr:\n")
			for _, line := range strings.Split(truncateOutput(d.Stderr, 500), "\n") {
				fmt.Printf("        %s\n", line)
			}
		} else {
			fmt.Printf("      stderr: (empty)\n")
		}
	}
	if d.Stdout != "" {
		fmt.Printf("      stdout:\n")
		for _, line := range strings.Split(truncateOutput(d.Stdout, 500), "\n") {
			fmt.Printf("        %s\n", line)
		}
	}

	// For HTTP tests
	if d.Method != "" {
		fmt.Printf("      request: %s %s\n", d.Method, d.Path)
	}
	if d.StatusCode != 0 {
		fmt.Printf("      status: %d\n", d.StatusCode)
	}
	if d.Body != "" && d.Method != "" {
		fmt.Printf("      response body:\n")
		for _, line := range strings.Split(truncateOutput(d.Body, 500), "\n") {
			fmt.Printf("        %s\n", line)
		}
	}
}

// printIndentedOutput prints output with proper indentation.
func (o *Output) printIndentedOutput(s string, maxLen int) {
	s = truncateOutput(s, maxLen)
	for _, line := range strings.Split(s, "\n") {
		fmt.Printf("      %s\n", o.color(colorDim, line))
	}
}

// truncateOutput truncates a string and indicates how much was cut.
func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + fmt.Sprintf("\n... (%d bytes truncated)", len(s)-maxLen)
}

// detailsData holds extracted details data.
type detailsData struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	Args       []string
	Method     string
	Path       string
	StatusCode int
	Body       string
}

// extractDetails extracts fields from a details struct using reflection.
func extractDetails(details interface{}) *detailsData {
	if details == nil {
		return nil
	}

	// Use reflection to access the fields since the details type is defined in runner.go
	v := reflect.ValueOf(details)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}

	d := &detailsData{}

	if f := v.FieldByName("Stdout"); f.IsValid() && f.Kind() == reflect.String {
		d.Stdout = f.String()
	}
	if f := v.FieldByName("Stderr"); f.IsValid() && f.Kind() == reflect.String {
		d.Stderr = f.String()
	}
	if f := v.FieldByName("ExitCode"); f.IsValid() && f.Kind() == reflect.Int {
		d.ExitCode = int(f.Int())
	}
	if f := v.FieldByName("Args"); f.IsValid() && f.Kind() == reflect.Slice {
		for i := 0; i < f.Len(); i++ {
			if elem := f.Index(i); elem.Kind() == reflect.String {
				d.Args = append(d.Args, elem.String())
			}
		}
	}
	if f := v.FieldByName("Method"); f.IsValid() && f.Kind() == reflect.String {
		d.Method = f.String()
	}
	if f := v.FieldByName("Path"); f.IsValid() && f.Kind() == reflect.String {
		d.Path = f.String()
	}
	if f := v.FieldByName("StatusCode"); f.IsValid() && f.Kind() == reflect.Int {
		d.StatusCode = int(f.Int())
	}
	if f := v.FieldByName("Body"); f.IsValid() && f.Kind() == reflect.String {
		d.Body = f.String()
	}

	return d
}

// PrintBuildProgress prints build progress.
func (o *Output) PrintBuildProgress(current, total int, name string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.isTTY {
		fmt.Printf("%s Building %s[%d/%d]%s %s%s\n",
			o.color(colorYellow, "⚡"),
			o.color(colorDim, ""),
			current, total,
			colorReset,
			name,
			clearToEnd)
	}
}

// StartSpinner starts an animated spinner with a message.
func (o *Output) StartSpinner(message string) {
	if !o.isTTY {
		fmt.Printf("  %s...\n", message)
		return
	}

	o.mu.Lock()
	if o.spinnerStop != nil {
		o.mu.Unlock()
		return // Spinner already running
	}
	o.spinnerStop = make(chan struct{})
	o.spinnerDone = make(chan struct{})
	o.mu.Unlock()

	go func() {
		defer close(o.spinnerDone)
		frame := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		fmt.Print(hideCursor)
		defer fmt.Print(showCursor)

		for {
			select {
			case <-o.spinnerStop:
				fmt.Print(cursorToStart + clearLine)
				return
			case <-ticker.C:
				o.mu.Lock()
				fmt.Printf("%s  %s %s%s",
					cursorToStart,
					o.color(colorCyan, spinnerFrames[frame]),
					o.color(colorDim, message),
					clearToEnd)
				o.mu.Unlock()
				frame = (frame + 1) % len(spinnerFrames)
			}
		}
	}()
}

// StopSpinner stops the animated spinner.
func (o *Output) StopSpinner() {
	o.mu.Lock()
	if o.spinnerStop == nil {
		o.mu.Unlock()
		return
	}
	stop := o.spinnerStop
	done := o.spinnerDone
	o.spinnerStop = nil
	o.spinnerDone = nil
	o.mu.Unlock()

	close(stop)
	<-done
}

// Cleanup stops any running spinners and restores terminal state.
// Call this on program exit or interrupt.
func (o *Output) Cleanup() {
	o.StopSpinner()
	if o.isTTY {
		// Ensure cursor is visible and line is cleared
		fmt.Print(showCursor + cursorToStart + clearLine)
	}
}

// PrintResults prints the final test results summary.
func (o *Output) PrintResults(results *Results) {
	o.mu.Lock()
	defer o.mu.Unlock()

	fmt.Println()
	if o.isTTY {
		if results.Failed > 0 {
			fmt.Println(o.color(colorRed+colorBold, "╭"+strings.Repeat("─", 40)+"╮"))
			fmt.Printf("%s%s%s\n",
				o.color(colorRed+colorBold, "│"),
				o.color(colorRed+colorBold, padCenter(fmt.Sprintf("FAILED: %d/%d tests passed", results.Passed, results.Total), 40)),
				o.color(colorRed+colorBold, "│"))
			fmt.Println(o.color(colorRed+colorBold, "╰"+strings.Repeat("─", 40)+"╯"))
		} else {
			fmt.Println(o.color(colorGreen+colorBold, "╭"+strings.Repeat("─", 40)+"╮"))
			fmt.Printf("%s%s%s\n",
				o.color(colorGreen+colorBold, "│"),
				o.color(colorGreen+colorBold, padCenter(fmt.Sprintf("PASSED: %d/%d tests", results.Passed, results.Total), 40)),
				o.color(colorGreen+colorBold, "│"))
			fmt.Println(o.color(colorGreen+colorBold, "╰"+strings.Repeat("─", 40)+"╯"))
		}
		fmt.Printf("  %s %d examples in %s\n",
			o.color(colorDim, "Completed"),
			len(results.Examples),
			results.Duration.Round(time.Millisecond))
		fmt.Println()
	} else {
		if results.Failed > 0 {
			fmt.Printf("FAILED: %d/%d tests passed (%d examples, %s)\n",
				results.Passed, results.Total, len(results.Examples), results.Duration.Round(time.Millisecond))
		} else {
			fmt.Printf("PASSED: %d/%d tests (%d examples, %s)\n",
				results.Passed, results.Total, len(results.Examples), results.Duration.Round(time.Millisecond))
		}
	}
}
