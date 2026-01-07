// Package starui provides Starlark-based UI customization for ccapp.
// It implements a React and Tailwind inspired API for building interfaces.
package starui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"go.starlark.net/starlark"
)

// AppContext provides access to application state from Starlark scripts.
type AppContext struct {
	// Application info
	AppTitle   string
	BundlesDir string

	// Bundle data
	Bundles []BundleInfo

	// Logo for rendering
	Logo *graphics.SVG

	// Text renderer
	Text *text.Renderer

	// Callbacks for actions
	OnOpenLogs    func()
	OnSelectBundle func(index int)
	OnStopVM      func()
	OnBack        func()

	// Current screen state
	CurrentScreen string
	ErrorMessage  string
	BootName      string
}

// BundleInfo represents a discovered bundle.
type BundleInfo struct {
	Index       int
	Name        string
	Description string
	Dir         string
}

// Screen represents a rendered screen from Starlark.
type Screen struct {
	Root ui.Widget
}

// Engine manages Starlark script execution for UI.
type Engine struct {
	thread    *starlark.Thread
	globals   starlark.StringDict
	appScript *starlark.Program
	ctx       *AppContext
}

// NewEngine creates a new Starlark UI engine.
func NewEngine() *Engine {
	return &Engine{}
}

// LoadScript loads and compiles a Starlark script.
func (e *Engine) LoadScript(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read script: %w", err)
	}

	return e.LoadScriptFromString(filepath.Base(path), string(data))
}

// LoadScriptFromString loads and compiles a Starlark script from a string.
func (e *Engine) LoadScriptFromString(name, source string) error {
	_, prog, err := starlark.SourceProgram(name, source, e.predeclared().Has)
	if err != nil {
		return fmt.Errorf("compile script: %w", err)
	}

	e.appScript = prog
	return nil
}

// predeclared returns the predeclared names available to scripts.
func (e *Engine) predeclared() starlark.StringDict {
	return starlark.StringDict{
		// Layout components (React-like)
		"Row":      starlark.NewBuiltin("Row", builtinRow),
		"Column":   starlark.NewBuiltin("Column", builtinColumn),
		"Stack":    starlark.NewBuiltin("Stack", builtinStack),
		"Center":   starlark.NewBuiltin("Center", builtinCenter),
		"Padding":  starlark.NewBuiltin("Padding", builtinPadding),
		"Spacer":   starlark.NewBuiltin("Spacer", builtinSpacer),
		"Align":    starlark.NewBuiltin("Align", builtinAlign),

		// Content components
		"Text":   starlark.NewBuiltin("Text", builtinText),
		"Box":    starlark.NewBuiltin("Box", builtinBox),
		"Button": starlark.NewBuiltin("Button", builtinButton),
		"Card":   starlark.NewBuiltin("Card", builtinCard),
		"Logo":   starlark.NewBuiltin("Logo", builtinLogo),

		// Scroll components
		"ScrollView": starlark.NewBuiltin("ScrollView", builtinScrollView),

		// Style utilities (Tailwind-like)
		"rgb":  starlark.NewBuiltin("rgb", builtinRGB),
		"rgba": starlark.NewBuiltin("rgba", builtinRGBA),

		// Tailwind color palette
		"gray":    starlark.NewBuiltin("gray", builtinColorGray),
		"red":     starlark.NewBuiltin("red", builtinColorRed),
		"orange":  starlark.NewBuiltin("orange", builtinColorOrange),
		"yellow":  starlark.NewBuiltin("yellow", builtinColorYellow),
		"green":   starlark.NewBuiltin("green", builtinColorGreen),
		"blue":    starlark.NewBuiltin("blue", builtinColorBlue),
		"indigo":  starlark.NewBuiltin("indigo", builtinColorIndigo),
		"purple":  starlark.NewBuiltin("purple", builtinColorPurple),
		"pink":    starlark.NewBuiltin("pink", builtinColorPink),
		"white":   starlark.NewBuiltin("white", builtinColorWhite),
		"black":   starlark.NewBuiltin("black", builtinColorBlack),
		"transparent": starlark.NewBuiltin("transparent", builtinColorTransparent),

		// Spacing utilities
		"insets": starlark.NewBuiltin("insets", builtinInsets),

		// Alignment constants
		"top_left":      starlark.String("top_left"),
		"top_center":    starlark.String("top_center"),
		"top_right":     starlark.String("top_right"),
		"center_left":   starlark.String("center_left"),
		"center_center": starlark.String("center_center"),
		"center_right":  starlark.String("center_right"),
		"bottom_left":   starlark.String("bottom_left"),
		"bottom_center": starlark.String("bottom_center"),
		"bottom_right":  starlark.String("bottom_right"),

		// Main axis alignment
		"main_start":         starlark.String("main_start"),
		"main_center":        starlark.String("main_center"),
		"main_end":           starlark.String("main_end"),
		"main_space_between": starlark.String("main_space_between"),
		"main_space_around":  starlark.String("main_space_around"),
		"main_space_evenly":  starlark.String("main_space_evenly"),

		// Cross axis alignment
		"cross_start":   starlark.String("cross_start"),
		"cross_center":  starlark.String("cross_center"),
		"cross_end":     starlark.String("cross_end"),
		"cross_stretch": starlark.String("cross_stretch"),

		// Built-in functions
		"print": starlark.NewBuiltin("print", builtinPrint),
	}
}

// SetContext sets the application context for rendering.
func (e *Engine) SetContext(ctx *AppContext) {
	e.ctx = ctx
}

// RenderScreen executes the script and renders a specific screen.
func (e *Engine) RenderScreen(screenName string) (*Screen, error) {
	if e.appScript == nil {
		return nil, fmt.Errorf("no script loaded")
	}

	thread := &starlark.Thread{
		Name: "starui",
		Print: func(_ *starlark.Thread, msg string) {
			fmt.Println("[starui]", msg)
		},
	}
	thread.SetLocal("ctx", e.ctx)

	globals, err := e.appScript.Init(thread, e.predeclared())
	if err != nil {
		return nil, fmt.Errorf("init script: %w", err)
	}

	// Look for the screen function
	fnName := screenName + "_screen"
	fnVal, ok := globals[fnName]
	if !ok {
		return nil, fmt.Errorf("screen function %q not found", fnName)
	}

	fn, ok := fnVal.(starlark.Callable)
	if !ok {
		return nil, fmt.Errorf("%q is not a function", fnName)
	}

	// Build context dict to pass to the function
	ctxDict := e.buildContextDict()

	// Call the screen function
	result, err := starlark.Call(thread, fn, starlark.Tuple{ctxDict}, nil)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", fnName, err)
	}

	// Extract widget from result
	widgetVal, ok := result.(*WidgetValue)
	if !ok {
		return nil, fmt.Errorf("screen function must return a widget, got %s", result.Type())
	}

	return &Screen{Root: widgetVal.Widget}, nil
}

// buildContextDict creates a Starlark dict with app context.
func (e *Engine) buildContextDict() *starlark.Dict {
	ctx := e.ctx
	if ctx == nil {
		ctx = &AppContext{}
	}

	dict := starlark.NewDict(10)

	dict.SetKey(starlark.String("app_title"), starlark.String(ctx.AppTitle))
	dict.SetKey(starlark.String("bundles_dir"), starlark.String(ctx.BundlesDir))
	dict.SetKey(starlark.String("current_screen"), starlark.String(ctx.CurrentScreen))
	dict.SetKey(starlark.String("error_message"), starlark.String(ctx.ErrorMessage))
	dict.SetKey(starlark.String("boot_name"), starlark.String(ctx.BootName))

	// Bundle list
	bundleList := make([]starlark.Value, len(ctx.Bundles))
	for i, b := range ctx.Bundles {
		bundleDict := starlark.NewDict(4)
		bundleDict.SetKey(starlark.String("index"), starlark.MakeInt(b.Index))
		bundleDict.SetKey(starlark.String("name"), starlark.String(b.Name))
		bundleDict.SetKey(starlark.String("description"), starlark.String(b.Description))
		bundleDict.SetKey(starlark.String("dir"), starlark.String(b.Dir))
		bundleList[i] = bundleDict
	}
	dict.SetKey(starlark.String("bundles"), starlark.NewList(bundleList))

	// Action callbacks
	dict.SetKey(starlark.String("open_logs"), starlark.NewBuiltin("open_logs", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if ctx.OnOpenLogs != nil {
			ctx.OnOpenLogs()
		}
		return starlark.None, nil
	}))

	dict.SetKey(starlark.String("select_bundle"), starlark.NewBuiltin("select_bundle", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var index int
		if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "index", &index); err != nil {
			return nil, err
		}
		if ctx.OnSelectBundle != nil {
			ctx.OnSelectBundle(index)
		}
		return starlark.None, nil
	}))

	dict.SetKey(starlark.String("stop_vm"), starlark.NewBuiltin("stop_vm", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if ctx.OnStopVM != nil {
			ctx.OnStopVM()
		}
		return starlark.None, nil
	}))

	dict.SetKey(starlark.String("back"), starlark.NewBuiltin("back", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if ctx.OnBack != nil {
			ctx.OnBack()
		}
		return starlark.None, nil
	}))

	return dict
}

// HasScreen checks if a screen function exists in the loaded script.
func (e *Engine) HasScreen(screenName string) bool {
	if e.appScript == nil {
		return false
	}

	thread := &starlark.Thread{Name: "check"}
	globals, err := e.appScript.Init(thread, e.predeclared())
	if err != nil {
		return false
	}

	fnName := screenName + "_screen"
	_, ok := globals[fnName]
	return ok
}

// FindAppStar looks for app.star in the bundles directory.
func FindAppStar(bundlesDir string) string {
	path := filepath.Join(bundlesDir, "app.star")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}
