package rtg

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/tinyrange/cc/internal/asm"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
)

// TypeKind enumerates the minimal scalar types supported by the rtg front-end.
type TypeKind int

const (
	TypeInvalid TypeKind = iota
	TypeI64
	TypeI32
	TypeI16
	TypeI8
	TypeBool
	TypeUintptr
	TypeString
	TypeLabel
	TypeBuffer // Stack-allocated byte buffer
)

type Type struct {
	Kind       TypeKind
	BufferSize int64 // Size in bytes for TypeBuffer
}

func (t Type) String() string {
	switch t.Kind {
	case TypeI64:
		return "int64"
	case TypeI32:
		return "int32"
	case TypeI16:
		return "int16"
	case TypeI8:
		return "int8"
	case TypeBool:
		return "bool"
	case TypeUintptr:
		return "uintptr"
	case TypeString:
		return "string"
	case TypeLabel:
		return "label"
	case TypeBuffer:
		return fmt.Sprintf("[%d]byte", t.BufferSize)
	default:
		return "invalid"
	}
}

type scope struct {
	parent *scope
	vars   map[string]Type
}

func newScope(parent *scope) *scope {
	return &scope{
		parent: parent,
		vars:   make(map[string]Type),
	}
}

func (s *scope) lookup(name string) (Type, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if t, ok := cur.vars[name]; ok {
			return t, true
		}
	}
	return Type{}, false
}

func (s *scope) define(name string, typ Type) error {
	if name == "" {
		return fmt.Errorf("rtg: empty identifier")
	}
	if _, exists := s.vars[name]; exists {
		return fmt.Errorf("rtg: identifier %q already defined", name)
	}
	s.vars[name] = typ
	return nil
}

// CompileOptions specifies options for RTG compilation.
type CompileOptions struct {
	// GOARCH is the target architecture ("amd64" or "arm64").
	// Used to resolve runtime.GOARCH comparisons at compile time.
	GOARCH string

	// Flags holds compile-time flags for conditional compilation.
	// Used with runtime.Ifdef("flag") to include/exclude code at compile time.
	// Undefined flags are treated as false.
	Flags map[string]bool

	// Config holds compile-time configuration values.
	// Used with runtime.Config("key") to inject values at compile time.
	// Supported types: string, int64, []string
	Config map[string]any
}

// bufferInfo tracks a stack-allocated buffer for code generation.
type bufferInfo struct {
	name   string
	size   int64
	slotID int
}

// funcSignature holds information about a function's signature.
type funcSignature struct {
	params     []string // parameter names in order
	returnType Type     // return type (TypeInvalid if void)
}

// Compiler holds the state for a single source-to-IR lowering.
type Compiler struct {
	fset        *token.FileSet
	file        *ast.File
	scope       *scope
	returnType  Type
	opts        CompileOptions
	constants   map[string]int64      // package-level constants
	funcSigs    map[string]funcSignature // function signatures
	labelCount  int                   // counter for generating unique labels
	buffers     []bufferInfo          // stack buffers declared in current function
	nextSlotID  int                   // counter for unique slot IDs
	embedVarCtr int                   // counter for unique embed variable names
}

// CompileProgram parses src and lowers it into an ir.Program. The accepted
// language is intentionally small; unsupported constructs return friendly
// errors rather than attempting partial lowering.
func CompileProgram(src string) (*ir.Program, error) {
	return CompileProgramWithOptions(src, CompileOptions{})
}

// CompileProgramWithOptions parses src and lowers it into an ir.Program
// with the specified compilation options.
func CompileProgramWithOptions(src string, opts CompileOptions) (*ir.Program, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "input.go", src, parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("rtg: parse: %w", err)
	}

	c := &Compiler{
		fset:      fset,
		file:      file,
		scope:     newScope(nil),
		opts:      opts,
		constants: make(map[string]int64),
		funcSigs:  make(map[string]funcSignature),
	}
	return c.compile()
}

func (c *Compiler) compile() (*ir.Program, error) {
	pkgName := ""
	if c.file.Name != nil {
		pkgName = c.file.Name.Name
	}
	if pkgName != "main" {
		return nil, fmt.Errorf("rtg: only package main is supported (got %q)", pkgName)
	}

	// First pass: collect imports and constants
	for _, decl := range c.file.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok {
			if err := c.processGenDecl(genDecl); err != nil {
				return nil, err
			}
		}
	}

	// Second pass: collect function signatures
	for _, decl := range c.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		sig, err := c.extractFuncSignature(fn)
		if err != nil {
			return nil, err
		}
		c.funcSigs[fn.Name.Name] = sig
	}

	// Third pass: compile functions
	methods := make(map[string]ir.Method)
	var entrypoint string

	for _, decl := range c.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue // skip GenDecl, already processed
		}
		name := fn.Name.Name
		if _, exists := methods[name]; exists {
			return nil, fmt.Errorf("rtg: duplicate function %q", name)
		}

		method, err := c.lowerFunc(fn)
		if err != nil {
			return nil, err
		}
		methods[name] = method
		if entrypoint == "" {
			entrypoint = name
		}
	}

	if entrypoint == "" {
		return nil, fmt.Errorf("rtg: no functions found")
	}

	return &ir.Program{
		Entrypoint: entrypoint,
		Methods:    methods,
	}, nil
}

// processGenDecl handles import and const declarations.
func (c *Compiler) processGenDecl(decl *ast.GenDecl) error {
	switch decl.Tok {
	case token.IMPORT:
		// Allow imports but only for the runtime package (which we handle specially)
		for _, spec := range decl.Specs {
			importSpec, ok := spec.(*ast.ImportSpec)
			if !ok {
				continue
			}
			path, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				return fmt.Errorf("rtg: invalid import path: %w", err)
			}
			// Only allow the RTG runtime package
			if path != "github.com/tinyrange/cc/internal/rtg/runtime" {
				return fmt.Errorf("rtg: unsupported import %q (only github.com/tinyrange/cc/internal/rtg/runtime is allowed)", path)
			}
			// Import is allowed but we handle runtime.* specially
		}
		return nil

	case token.CONST:
		// Process constant declarations
		for _, spec := range decl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range valueSpec.Names {
				if i >= len(valueSpec.Values) {
					// No value provided, skip
					continue
				}
				val, err := c.evalInt(valueSpec.Values[i])
				if err != nil {
					return fmt.Errorf("rtg: const %s: %w", name.Name, err)
				}
				c.constants[name.Name] = val
			}
		}
		return nil

	case token.VAR:
		// Disallow var declarations at package level
		return fmt.Errorf("rtg: package-level var declarations are not supported")

	case token.TYPE:
		// Disallow type declarations
		return fmt.Errorf("rtg: type declarations are not supported")

	default:
		return fmt.Errorf("rtg: unsupported declaration %v", decl.Tok)
	}
}

// extractFuncSignature extracts a function's signature (parameter names and return type).
func (c *Compiler) extractFuncSignature(fn *ast.FuncDecl) (funcSignature, error) {
	var sig funcSignature

	// Extract return type
	if fn.Type.Results != nil && fn.Type.Results.NumFields() > 0 {
		if fn.Type.Results.NumFields() > 1 {
			return sig, fmt.Errorf("rtg: multiple return values are not supported (%s)", fn.Name.Name)
		}
		field := fn.Type.Results.List[0]
		typ, err := resolveType(field.Type)
		if err != nil {
			return sig, fmt.Errorf("rtg: result type: %w", err)
		}
		sig.returnType = typ
	}

	// Extract parameter names
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			if len(field.Names) != 1 {
				return sig, fmt.Errorf("rtg: parameters must be named (%s)", fn.Name.Name)
			}
			sig.params = append(sig.params, field.Names[0].Name)
		}
	}

	return sig, nil
}

func (c *Compiler) lowerFunc(fn *ast.FuncDecl) (ir.Method, error) {
	if fn.Recv != nil {
		return nil, fmt.Errorf("rtg: methods are not supported (%s)", fn.Name.Name)
	}
	if fn.Type.TypeParams != nil && fn.Type.TypeParams.NumFields() > 0 {
		return nil, fmt.Errorf("rtg: type parameters are not supported (%s)", fn.Name.Name)
	}

	var retType Type
	if fn.Type.Results != nil && fn.Type.Results.NumFields() > 0 {
		if fn.Type.Results.NumFields() > 1 {
			return nil, fmt.Errorf("rtg: multiple return values are not supported (%s)", fn.Name.Name)
		}
		field := fn.Type.Results.List[0]
		if len(field.Names) > 0 {
			return nil, fmt.Errorf("rtg: named result parameters are not supported (%s)", fn.Name.Name)
		}
		typ, err := resolveType(field.Type)
		if err != nil {
			return nil, fmt.Errorf("rtg: result type: %w", err)
		}
		retType = typ
	}

	prevScope := c.scope
	c.scope = newScope(prevScope)
	prevRet := c.returnType
	c.returnType = retType
	prevBuffers := c.buffers
	c.buffers = nil // Reset buffers for this function
	defer func() {
		c.scope = prevScope
		c.returnType = prevRet
		c.buffers = prevBuffers
	}()

	if fn.Body == nil {
		return nil, fmt.Errorf("rtg: function %q has no body", fn.Name.Name)
	}

	var method ir.Method
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			if len(field.Names) != 1 {
				return nil, fmt.Errorf("rtg: parameters must be named (%s)", fn.Name.Name)
			}
			name := field.Names[0].Name
			typ, err := resolveType(field.Type)
			if err != nil {
				return nil, fmt.Errorf("rtg: parameter %s: %w", name, err)
			}
			if err := c.scope.define(name, typ); err != nil {
				return nil, fmt.Errorf("rtg: parameter %s: %w", name, err)
			}
			method = append(method, ir.DeclareParam(name))
		}
	}

	sawReturn := false
	for _, stmt := range fn.Body.List {
		frags, err := c.lowerStmt(stmt)
		if err != nil {
			return nil, err
		}
		for _, f := range frags {
			if _, ok := f.(ir.ReturnFragment); ok {
				sawReturn = true
			}
			method = append(method, f)
		}
	}

	if retType.Kind != TypeInvalid && !sawReturn {
		// Auto-insert return with zero value
		method = append(method, ir.Return(ir.Int64(0)))
	}

	// Wrap method body with stack slots for any declared buffers
	// We need to wrap from innermost to outermost (reverse order)
	if len(c.buffers) > 0 {
		method = c.wrapWithStackSlots(method)
	}

	return method, nil
}

// wrapWithStackSlots wraps the method body with ir.WithStackSlot for each buffer.
// Buffers are wrapped from innermost (last declared) to outermost (first declared).
func (c *Compiler) wrapWithStackSlots(body ir.Method) ir.Method {
	// The body should be wrapped in nested WithStackSlot calls
	// For each buffer, create a WithStackSlot that wraps the current body
	result := ir.Block(body)

	// Wrap in reverse order so first buffer is outermost
	for i := len(c.buffers) - 1; i >= 0; i-- {
		buf := c.buffers[i]
		slotID := buf.slotID
		size := buf.size
		innerBody := result

		result = ir.Block{
			ir.WithStackSlot(ir.StackSlotConfig{
				Size: size,
				Body: func(slot ir.StackSlot) ir.Fragment {
					// Replace slot references in the body with the actual slot
					return replaceSlotRefs(innerBody, slotID, slot)
				},
			}),
		}
	}

	return ir.Method{result}
}

// replaceSlotRefs replaces placeholder slot references with actual slot references.
func replaceSlotRefs(frag ir.Fragment, slotID int, slot ir.StackSlot) ir.Fragment {
	switch f := frag.(type) {
	case ir.Block:
		result := make(ir.Block, len(f))
		for i, inner := range f {
			result[i] = replaceSlotRefs(inner, slotID, slot)
		}
		return result
	case ir.Method:
		result := make(ir.Method, len(f))
		for i, inner := range f {
			result[i] = replaceSlotRefs(inner, slotID, slot)
		}
		return result
	case ir.AssignFragment:
		return ir.Assign(
			replaceSlotRefs(f.Dst, slotID, slot),
			replaceSlotRefs(f.Src, slotID, slot),
		)
	case ir.IfFragment:
		then := replaceSlotRefs(f.Then, slotID, slot)
		var otherwise ir.Fragment
		if f.Otherwise != nil {
			otherwise = replaceSlotRefs(f.Otherwise, slotID, slot)
		}
		return ir.IfFragment{
			Cond:      replaceSlotRefsInCondition(f.Cond, slotID, slot),
			Then:      then,
			Otherwise: otherwise,
		}
	case ir.LabelFragment:
		return ir.LabelFragment{
			Label: f.Label,
			Block: replaceSlotRefs(f.Block, slotID, slot).(ir.Block),
		}
	case ir.SyscallFragment:
		args := make([]ir.Fragment, len(f.Args))
		for i, arg := range f.Args {
			args[i] = replaceSlotRefs(arg, slotID, slot).(ir.Fragment)
		}
		return ir.SyscallFragment{Num: f.Num, Args: args}
	case ir.PrintfFragment:
		args := make([]ir.Fragment, len(f.Args))
		for i, arg := range f.Args {
			args[i] = replaceSlotRefs(arg, slotID, slot).(ir.Fragment)
		}
		return ir.PrintfFragment{Format: f.Format, Args: args}
	case ir.ReturnFragment:
		return ir.ReturnFragment{Value: replaceSlotRefs(f.Value, slotID, slot)}
	case ir.GotoFragment:
		return f
	case ir.OpFragment:
		return ir.OpFragment{
			Kind:  f.Kind,
			Left:  replaceSlotRefs(f.Left, slotID, slot),
			Right: replaceSlotRefs(f.Right, slotID, slot),
		}
	case ir.CallFragment:
		var args []ir.Fragment
		if len(f.Args) > 0 {
			args = make([]ir.Fragment, len(f.Args))
			for i, arg := range f.Args {
				args[i] = replaceSlotRefs(arg, slotID, slot).(ir.Fragment)
			}
		}
		return ir.CallFragment{
			Target: replaceSlotRefs(f.Target, slotID, slot),
			Args:   args,
			Result: f.Result,
		}
	case bufferMemPlaceholder:
		if f.slotID == slotID {
			// Get the base StackSlotMemFragment from slot.At()
			mem := slot.At(f.disp).(ir.StackSlotMemFragment)
			switch f.width {
			case 8:
				return mem.As8()
			case 16:
				return mem.As16()
			case 32:
				return mem.As32()
			default:
				return mem // 64-bit (default)
			}
		}
		return f
	case bufferPtrPlaceholder:
		if f.slotID == slotID {
			if f.disp == 0 {
				return slot.Pointer()
			}
			return slot.PointerWithDisp(ir.Int64(f.disp))
		}
		return f
	default:
		return frag
	}
}

// replaceSlotRefsInCondition handles conditions in if statements.
func replaceSlotRefsInCondition(cond ir.Condition, slotID int, slot ir.StackSlot) ir.Condition {
	switch c := cond.(type) {
	case ir.CompareCondition:
		return ir.CompareCondition{
			Kind:  c.Kind,
			Left:  replaceSlotRefs(c.Left, slotID, slot),
			Right: replaceSlotRefs(c.Right, slotID, slot),
		}
	case ir.IsNegativeCondition:
		return ir.IsNegativeCondition{Value: replaceSlotRefs(c.Value, slotID, slot)}
	case ir.IsZeroCondition:
		return ir.IsZeroCondition{Value: replaceSlotRefs(c.Value, slotID, slot)}
	default:
		return cond
	}
}

// bufferPtrPlaceholder is a placeholder for buffer pointer references.
// It is used during lowering and replaced with actual StackSlotPtrFragment
// when the function body is wrapped with WithStackSlot.
type bufferPtrPlaceholder struct {
	slotID int
	disp   int64
}

// bufferMemPlaceholder is a placeholder for buffer memory references.
// It is used during lowering and replaced with actual StackSlotMemFragment
// when the function body is wrapped with WithStackSlot.
type bufferMemPlaceholder struct {
	slotID int
	disp   int64
	width  int // 8, 16, 32, or 64 (0 means 64)
}

// findBuffer looks up a buffer by name in the current function.
func (c *Compiler) findBuffer(name string) *bufferInfo {
	for i := range c.buffers {
		if c.buffers[i].name == name {
			return &c.buffers[i]
		}
	}
	return nil
}

func resolveType(expr ast.Expr) (Type, error) {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "int64":
			return Type{Kind: TypeI64}, nil
		case "int32":
			return Type{Kind: TypeI32}, nil
		case "int16":
			return Type{Kind: TypeI16}, nil
		case "int8":
			return Type{Kind: TypeI8}, nil
		case "bool":
			return Type{Kind: TypeBool}, nil
		case "uintptr":
			return Type{Kind: TypeUintptr}, nil
		case "string":
			return Type{Kind: TypeString}, nil
		case "label":
			return Type{Kind: TypeLabel}, nil
		default:
			return Type{}, fmt.Errorf("unsupported type %q", t.Name)
		}
	case *ast.ArrayType:
		// Handle [N]byte array type for stack buffers
		elemType, ok := t.Elt.(*ast.Ident)
		if !ok || elemType.Name != "byte" {
			return Type{}, fmt.Errorf("only [N]byte arrays are supported")
		}
		// Get the array length
		lenExpr, ok := t.Len.(*ast.BasicLit)
		if !ok || lenExpr.Kind != token.INT {
			return Type{}, fmt.Errorf("array length must be a constant integer")
		}
		size, err := strconv.ParseInt(lenExpr.Value, 0, 64)
		if err != nil {
			return Type{}, fmt.Errorf("invalid array length: %w", err)
		}
		if size <= 0 {
			return Type{}, fmt.Errorf("array length must be positive")
		}
		return Type{Kind: TypeBuffer, BufferSize: size}, nil
	default:
		return Type{}, fmt.Errorf("unsupported type %T", expr)
	}
}

func (c *Compiler) lowerStmt(stmt ast.Stmt) ([]ir.Fragment, error) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return c.lowerExprStmt(s)
	case *ast.AssignStmt:
		return c.lowerAssign(s)
	case *ast.ReturnStmt:
		return c.lowerReturn(s)
	case *ast.DeclStmt:
		return c.lowerDecl(s)
	case *ast.IfStmt:
		return c.lowerIf(s)
	case *ast.ForStmt:
		return c.lowerFor(s)
	case *ast.LabeledStmt:
		return c.lowerLabeled(s)
	case *ast.BranchStmt:
		return c.lowerBranch(s)
	case *ast.EmptyStmt:
		return nil, nil
	default:
		return nil, fmt.Errorf("rtg: unsupported statement %T", stmt)
	}
}

func (c *Compiler) lowerDecl(stmt *ast.DeclStmt) ([]ir.Fragment, error) {
	decl, ok := stmt.Decl.(*ast.GenDecl)
	if !ok || decl.Tok != token.VAR {
		return nil, fmt.Errorf("rtg: only var declarations are supported")
	}
	if len(decl.Specs) != 1 {
		return nil, fmt.Errorf("rtg: only single var declarations are supported")
	}
	spec, ok := decl.Specs[0].(*ast.ValueSpec)
	if !ok {
		return nil, fmt.Errorf("rtg: unexpected declaration %T", decl.Specs[0])
	}
	if len(spec.Names) != 1 {
		return nil, fmt.Errorf("rtg: only single var declarations are supported")
	}
	if spec.Type == nil {
		return nil, fmt.Errorf("rtg: var declarations must specify a type")
	}

	typ, err := resolveType(spec.Type)
	if err != nil {
		return nil, err
	}
	name := spec.Names[0].Name

	// Handle buffer declarations specially
	if typ.Kind == TypeBuffer {
		if len(spec.Values) != 0 {
			return nil, fmt.Errorf("rtg: buffer declarations cannot have initializers")
		}
		// Register the buffer
		slotID := c.nextSlotID
		c.nextSlotID++
		c.buffers = append(c.buffers, bufferInfo{
			name:   name,
			size:   typ.BufferSize,
			slotID: slotID,
		})
		// Define the buffer variable in scope (it will resolve to a pointer placeholder)
		if err := c.scope.define(name, typ); err != nil {
			return nil, err
		}
		// No IR fragments generated here - the buffer is created by WithStackSlot
		return nil, nil
	}

	if err := c.scope.define(name, typ); err != nil {
		return nil, err
	}

	if len(spec.Values) == 0 {
		return nil, nil
	}
	if len(spec.Values) != 1 {
		return nil, fmt.Errorf("rtg: only single-value var declarations are supported")
	}

	value, valType, err := c.lowerExpr(spec.Values[0])
	if err != nil {
		return nil, err
	}
	if !typesCompatible(typ, valType) {
		return nil, fmt.Errorf("rtg: cannot assign %s to %s", valType, typ)
	}

	return []ir.Fragment{ir.Assign(ir.Var(name), value)}, nil
}

func (c *Compiler) lowerAssign(assign *ast.AssignStmt) ([]ir.Fragment, error) {
	// Handle multi-value assignment for embed functions: ptr, len := runtime.EmbedString("...")
	if len(assign.Lhs) == 2 && len(assign.Rhs) == 1 {
		if callExpr, ok := assign.Rhs[0].(*ast.CallExpr); ok {
			funcName, err := c.resolveFuncName(callExpr.Fun)
			if err == nil {
				switch funcName {
				case "EmbedString", "EmbedCString", "EmbedBytes", "EmbedConfigString", "EmbedConfigCString":
					return c.lowerEmbedAssign(assign, callExpr, funcName)
				}
			}
		}
	}

	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil, fmt.Errorf("rtg: only single-value assignments are supported")
	}

	// Handle index expression on left side: ptr[offset] = value
	if indexExpr, ok := assign.Lhs[0].(*ast.IndexExpr); ok {
		if assign.Tok != token.ASSIGN {
			return nil, fmt.Errorf("rtg: index assignment only supports = operator")
		}
		value, _, err := c.lowerExpr(assign.Rhs[0])
		if err != nil {
			return nil, err
		}
		dst, _, err := c.lowerIndex(indexExpr)
		if err != nil {
			return nil, err
		}
		return []ir.Fragment{ir.Assign(dst, value)}, nil
	}

	ident, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, fmt.Errorf("rtg: left-hand side must be identifier or index expression")
	}

	// Handle indirect call(target) or runtime.Call(target) specially - generate ir.Call with result variable
	if callExpr, ok := assign.Rhs[0].(*ast.CallExpr); ok {
		funcName, err := c.resolveFuncName(callExpr.Fun)
		if err == nil && (funcName == "call" || funcName == "Call") {
			if len(callExpr.Args) != 1 {
				return nil, fmt.Errorf("rtg: call expects (target)")
			}
			callTarget, _, err := c.lowerExpr(callExpr.Args[0])
			if err != nil {
				return nil, err
			}
			// Register the result variable
			valType := Type{Kind: TypeI64}
			switch assign.Tok {
			case token.DEFINE:
				if err := c.scope.define(ident.Name, valType); err != nil {
					return nil, err
				}
			case token.ASSIGN:
				existing, ok := c.scope.lookup(ident.Name)
				if !ok {
					return nil, fmt.Errorf("rtg: assignment to undefined identifier %q", ident.Name)
				}
				if !typesCompatible(existing, valType) {
					return nil, fmt.Errorf("rtg: cannot assign %s to %s", valType, existing)
				}
			default:
				return nil, fmt.Errorf("rtg: unsupported assignment operator %s", assign.Tok)
			}
			return []ir.Fragment{ir.Call(callTarget, ir.Var(ident.Name))}, nil
		}
	}

	value, valType, err := c.lowerExpr(assign.Rhs[0])
	if err != nil {
		return nil, err
	}

	switch assign.Tok {
	case token.DEFINE:
		if valType.Kind == TypeInvalid {
			valType = Type{Kind: TypeI64}
		}
		if err := c.scope.define(ident.Name, valType); err != nil {
			return nil, err
		}
	case token.ASSIGN:
		existing, ok := c.scope.lookup(ident.Name)
		if !ok {
			return nil, fmt.Errorf("rtg: assignment to undefined identifier %q", ident.Name)
		}
		if !typesCompatible(existing, valType) {
			return nil, fmt.Errorf("rtg: cannot assign %s to %s", valType, existing)
		}
	default:
		return nil, fmt.Errorf("rtg: unsupported assignment operator %s", assign.Tok)
	}

	return []ir.Fragment{ir.Assign(ir.Var(ident.Name), value)}, nil
}

func (c *Compiler) lowerIf(stmt *ast.IfStmt) ([]ir.Fragment, error) {
	if stmt.Init != nil {
		return nil, fmt.Errorf("rtg: if init statements are not supported")
	}

	cond, err := c.lowerCondition(stmt.Cond)
	if err != nil {
		return nil, err
	}

	thenBlock, err := c.lowerBlock(stmt.Body)
	if err != nil {
		return nil, err
	}

	if stmt.Else == nil {
		return []ir.Fragment{ir.If(cond, thenBlock)}, nil
	}

	elseBlock, err := c.lowerElse(stmt.Else)
	if err != nil {
		return nil, err
	}

	return []ir.Fragment{ir.If(cond, thenBlock, elseBlock)}, nil
}

func (c *Compiler) lowerFor(stmt *ast.ForStmt) ([]ir.Fragment, error) {
	if stmt.Init != nil {
		return nil, fmt.Errorf("rtg: for init statements are not supported")
	}
	if stmt.Post != nil {
		return nil, fmt.Errorf("rtg: for post statements are not supported")
	}

	// Generate unique labels for this loop
	c.labelCount++
	loopID := c.labelCount
	startLabel := ir.Label(fmt.Sprintf("__for_start_%d", loopID))
	endLabel := ir.Label(fmt.Sprintf("__for_end_%d", loopID))

	// Lower the loop body
	body, err := c.lowerBlock(stmt.Body)
	if err != nil {
		return nil, err
	}

	var frags []ir.Fragment

	if stmt.Cond == nil {
		// Infinite loop: for { ... }
		// start:
		//   body
		//   goto start
		loopBody := append(body, ir.Goto(startLabel))
		frags = append(frags, ir.DeclareLabel(startLabel, ir.Block(loopBody)))
	} else {
		// Conditional loop: for cond { ... }
		// start:
		//   if !cond { goto end }
		//   body
		//   goto start
		// end:
		cond, err := c.lowerCondition(stmt.Cond)
		if err != nil {
			return nil, err
		}

		// Invert the condition for the exit check
		exitCond := invertCondition(cond)

		loopBody := ir.Block{
			ir.If(exitCond, ir.Block{ir.Goto(endLabel)}),
		}
		loopBody = append(loopBody, body...)
		loopBody = append(loopBody, ir.Goto(startLabel))

		frags = append(frags, ir.DeclareLabel(startLabel, loopBody))
		frags = append(frags, ir.DeclareLabel(endLabel, ir.Block{}))
	}

	return frags, nil
}

// invertCondition inverts a comparison condition for loop exit checks.
func invertCondition(cond ir.Condition) ir.Condition {
	if cc, ok := cond.(ir.CompareCondition); ok {
		switch cc.Kind {
		case ir.CompareEqual:
			return ir.CompareCondition{Kind: ir.CompareNotEqual, Left: cc.Left, Right: cc.Right}
		case ir.CompareNotEqual:
			return ir.CompareCondition{Kind: ir.CompareEqual, Left: cc.Left, Right: cc.Right}
		case ir.CompareLess:
			return ir.CompareCondition{Kind: ir.CompareGreaterOrEqual, Left: cc.Left, Right: cc.Right}
		case ir.CompareLessOrEqual:
			return ir.CompareCondition{Kind: ir.CompareGreater, Left: cc.Left, Right: cc.Right}
		case ir.CompareGreater:
			return ir.CompareCondition{Kind: ir.CompareLessOrEqual, Left: cc.Left, Right: cc.Right}
		case ir.CompareGreaterOrEqual:
			return ir.CompareCondition{Kind: ir.CompareLess, Left: cc.Left, Right: cc.Right}
		}
	}
	// Fallback: compare with 0 (treat as false)
	return ir.CompareCondition{Kind: ir.CompareEqual, Left: cond, Right: ir.Int64(0)}
}

func (c *Compiler) lowerElse(stmt ast.Stmt) (ir.Fragment, error) {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		return c.lowerBlock(s)
	case *ast.IfStmt:
		frags, err := c.lowerIf(s)
		if err != nil {
			return nil, err
		}
		if len(frags) != 1 {
			return nil, fmt.Errorf("rtg: expected single fragment in else-if lowering")
		}
		return frags[0], nil
	default:
		return nil, fmt.Errorf("rtg: unsupported else branch %T", stmt)
	}
}

func (c *Compiler) lowerBlock(block *ast.BlockStmt) (ir.Block, error) {
	var frags ir.Block
	for _, stmt := range block.List {
		lowered, err := c.lowerStmt(stmt)
		if err != nil {
			return nil, err
		}
		frags = append(frags, lowered...)
	}
	return frags, nil
}

func (c *Compiler) lowerLabeled(stmt *ast.LabeledStmt) ([]ir.Fragment, error) {
	body, err := c.lowerStmt(stmt.Stmt)
	if err != nil {
		return nil, err
	}
	return []ir.Fragment{ir.DeclareLabel(ir.Label(stmt.Label.Name), ir.Block(body))}, nil
}

func (c *Compiler) lowerBranch(stmt *ast.BranchStmt) ([]ir.Fragment, error) {
	if stmt.Tok != token.GOTO {
		return nil, fmt.Errorf("rtg: unsupported branch %s", stmt.Tok.String())
	}
	if stmt.Label == nil {
		return nil, fmt.Errorf("rtg: goto requires a label")
	}
	return []ir.Fragment{ir.Goto(ir.Label(stmt.Label.Name))}, nil
}

func (c *Compiler) lowerReturn(ret *ast.ReturnStmt) ([]ir.Fragment, error) {
	if len(ret.Results) > 1 {
		return nil, fmt.Errorf("rtg: multiple return values are not supported")
	}

	if len(ret.Results) == 0 {
		if c.returnType.Kind != TypeInvalid {
			return nil, fmt.Errorf("rtg: return value required (expected %s)", c.returnType)
		}
		return nil, nil
	}

	if c.returnType.Kind == TypeInvalid {
		return nil, fmt.Errorf("rtg: unexpected return value")
	}

	val, typ, err := c.lowerExpr(ret.Results[0])
	if err != nil {
		return nil, err
	}
	if !typesCompatible(c.returnType, typ) {
		return nil, fmt.Errorf("rtg: cannot return %s, expected %s", typ, c.returnType)
	}

	return []ir.Fragment{ir.Return(val)}, nil
}

func (c *Compiler) lowerExprStmt(stmt *ast.ExprStmt) ([]ir.Fragment, error) {
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok {
		return nil, fmt.Errorf("rtg: unsupported expression statement %T", stmt.X)
	}
	return c.lowerCallStmt(call)
}

func (c *Compiler) lowerCallStmt(call *ast.CallExpr) ([]ir.Fragment, error) {
	// Get the function name, handling both plain identifiers and runtime.X selectors
	funcName, err := c.resolveFuncName(call.Fun)
	if err != nil {
		return nil, err
	}

	switch funcName {
	case "printf", "Printf":
		return c.lowerPrintf(call)
	case "syscall", "Syscall":
		frag, _, err := c.lowerSyscall(call)
		if err != nil {
			return nil, err
		}
		return []ir.Fragment{frag}, nil
	case "store8", "Store8":
		frag, err := c.lowerStore(call, 8)
		if err != nil {
			return nil, err
		}
		return []ir.Fragment{frag}, nil
	case "store16", "Store16":
		frag, err := c.lowerStore(call, 16)
		if err != nil {
			return nil, err
		}
		return []ir.Fragment{frag}, nil
	case "store32", "Store32":
		frag, err := c.lowerStore(call, 32)
		if err != nil {
			return nil, err
		}
		return []ir.Fragment{frag}, nil
	case "store64", "Store64":
		frag, err := c.lowerStore(call, 64)
		if err != nil {
			return nil, err
		}
		return []ir.Fragment{frag}, nil
	case "gotoLabel", "GotoLabel":
		frag, err := c.lowerGotoCall(call)
		if err != nil {
			return nil, err
		}
		return []ir.Fragment{frag}, nil
	case "isb", "ISB":
		// Instruction Synchronization Barrier - required on ARM64 after modifying code
		if len(call.Args) != 0 {
			return nil, fmt.Errorf("rtg: isb() takes no arguments")
		}
		return []ir.Fragment{ir.ISB()}, nil
	case "cacheFlush", "CacheFlush":
		// Cache flush for self-modifying code
		if len(call.Args) != 2 {
			return nil, fmt.Errorf("rtg: cacheFlush(base, size) takes 2 arguments")
		}
		baseFrag, _, err := c.lowerExpr(call.Args[0])
		if err != nil {
			return nil, fmt.Errorf("rtg: cacheFlush base: %w", err)
		}
		sizeFrag, _, err := c.lowerExpr(call.Args[1])
		if err != nil {
			return nil, fmt.Errorf("rtg: cacheFlush size: %w", err)
		}
		return []ir.Fragment{ir.CacheFlush(baseFrag, sizeFrag)}, nil
	case "logKmsg", "LogKmsg":
		return c.lowerLogKmsg(call)
	default:
		// Check if it's a call to a user-defined function
		sig, ok := c.funcSigs[funcName]
		if !ok {
			return nil, fmt.Errorf("rtg: unknown function %q", funcName)
		}

		// Validate argument count
		if len(call.Args) != len(sig.params) {
			return nil, fmt.Errorf("rtg: function %q expects %d arguments, got %d", funcName, len(sig.params), len(call.Args))
		}

		// Allow calling user-defined functions as statements (void calls)
		if len(call.Args) == 0 {
			return []ir.Fragment{ir.CallMethod(funcName)}, nil
		}

		// Evaluate arguments
		args := make([]ir.Fragment, len(call.Args))
		for i, arg := range call.Args {
			frag, _, err := c.lowerExpr(arg)
			if err != nil {
				return nil, err
			}
			args[i] = frag
		}

		// Generate CallWithArgs using method pointer
		return []ir.Fragment{ir.CallWithArgs(ir.MethodPointer(funcName), args)}, nil
	}
}

// resolveFuncName extracts the function name from a call expression,
// handling both plain identifiers (syscall) and selector expressions (runtime.Syscall).
func (c *Compiler) resolveFuncName(fun ast.Expr) (string, error) {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name, nil
	case *ast.SelectorExpr:
		pkg, ok := f.X.(*ast.Ident)
		if !ok {
			return "", fmt.Errorf("rtg: unsupported call target")
		}
		if pkg.Name != "runtime" {
			return "", fmt.Errorf("rtg: unsupported package %q in call", pkg.Name)
		}
		return f.Sel.Name, nil
	default:
		return "", fmt.Errorf("rtg: unsupported call target %T", fun)
	}
}

func (c *Compiler) lowerExpr(expr ast.Expr) (ir.Fragment, Type, error) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		return c.lowerBasicLit(v)
	case *ast.Ident:
		return c.lowerIdent(v)
	case *ast.BinaryExpr:
		return c.lowerBinary(v)
	case *ast.CallExpr:
		return c.lowerExprCall(v)
	case *ast.ParenExpr:
		return c.lowerExpr(v.X)
	case *ast.UnaryExpr:
		return c.lowerUnary(v)
	case *ast.IndexExpr:
		return c.lowerIndex(v)
	case *ast.SelectorExpr:
		return c.lowerSelector(v)
	default:
		return nil, Type{}, fmt.Errorf("rtg: unsupported expression %T", expr)
	}
}

func (c *Compiler) lowerBasicLit(lit *ast.BasicLit) (ir.Fragment, Type, error) {
	switch lit.Kind {
	case token.INT:
		val, err := strconv.ParseInt(lit.Value, 0, 64)
		if err != nil {
			return nil, Type{}, fmt.Errorf("invalid int literal %q: %w", lit.Value, err)
		}
		return ir.Int64(val), Type{Kind: TypeI64}, nil
	case token.STRING:
		val, err := strconv.Unquote(lit.Value)
		if err != nil {
			return nil, Type{}, fmt.Errorf("invalid string literal %q: %w", lit.Value, err)
		}
		return val, Type{Kind: TypeString}, nil
	default:
		return nil, Type{}, fmt.Errorf("unsupported literal %s", lit.Kind.String())
	}
}

func (c *Compiler) lowerIdent(id *ast.Ident) (ir.Fragment, Type, error) {
	if typ, ok := c.scope.lookup(id.Name); ok {
		if typ.Kind == TypeLabel {
			return ir.Label(id.Name), typ, nil
		}
		if typ.Kind == TypeBuffer {
			// Buffer name used directly - return pointer to base
			buf := c.findBuffer(id.Name)
			if buf == nil {
				return nil, Type{}, fmt.Errorf("rtg: internal error: buffer %q not found", id.Name)
			}
			return bufferPtrPlaceholder{slotID: buf.slotID, disp: 0}, Type{Kind: TypeUintptr}, nil
		}
		return ir.Var(id.Name), typ, nil
	}
	switch id.Name {
	case "true":
		return ir.Int64(1), Type{Kind: TypeBool}, nil
	case "false":
		return ir.Int64(0), Type{Kind: TypeBool}, nil
	default:
		// Check for user-defined constants first
		if val, ok := c.constants[id.Name]; ok {
			return ir.Int64(val), Type{Kind: TypeI64}, nil
		}
		// Check for builtin constants
		if val, ok := constantValues[id.Name]; ok {
			return ir.Int64(val), Type{Kind: TypeI64}, nil
		}
		return nil, Type{}, fmt.Errorf("rtg: unknown identifier %q", id.Name)
	}
}

// lowerSelector handles selector expressions like runtime.X
func (c *Compiler) lowerSelector(sel *ast.SelectorExpr) (ir.Fragment, Type, error) {
	// Only handle runtime.X selectors
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil, Type{}, fmt.Errorf("rtg: unsupported selector expression")
	}
	if pkg.Name != "runtime" {
		return nil, Type{}, fmt.Errorf("rtg: unsupported package %q in selector", pkg.Name)
	}

	name := sel.Sel.Name

	// Handle runtime.GOARCH specially
	if name == "GOARCH" {
		if c.opts.GOARCH == "" {
			return nil, Type{}, fmt.Errorf("rtg: runtime.GOARCH used but GOARCH not specified in CompileOptions")
		}
		return c.opts.GOARCH, Type{Kind: TypeString}, nil
	}

	// Handle runtime constants (SYS_*, AT_FDCWD, etc.)
	if val, ok := constantValues[name]; ok {
		return ir.Int64(val), Type{Kind: TypeI64}, nil
	}

	// Handle syscall names (for runtime.SYS_* that maps to defs.Syscall)
	if strings.HasPrefix(name, "SYS_") {
		if num, ok := syscallNames[name]; ok {
			return ir.Int64(int64(num)), Type{Kind: TypeI64}, nil
		}
	}

	return nil, Type{}, fmt.Errorf("rtg: unknown runtime member %q", name)
}

func (c *Compiler) lowerUnary(expr *ast.UnaryExpr) (ir.Fragment, Type, error) {
	switch expr.Op {
	case token.AND:
		// Handle &buf[offset] for buffer pointers
		if indexExpr, ok := expr.X.(*ast.IndexExpr); ok {
			base, ok := indexExpr.X.(*ast.Ident)
			if ok {
				typ, ok := c.scope.lookup(base.Name)
				if ok && typ.Kind == TypeBuffer {
					buf := c.findBuffer(base.Name)
					if buf == nil {
						return nil, Type{}, fmt.Errorf("rtg: internal error: buffer %q not found", base.Name)
					}
					offset, err := c.evalInt(indexExpr.Index)
					if err != nil {
						return nil, Type{}, fmt.Errorf("rtg: buffer index must be a constant: %w", err)
					}
					return bufferPtrPlaceholder{slotID: buf.slotID, disp: offset}, Type{Kind: TypeUintptr}, nil
				}
			}
		}
		return nil, Type{}, fmt.Errorf("rtg: unsupported address-of expression")
	case token.SUB:
		if val, err := c.evalInt(expr.X); err == nil {
			return ir.Int64(-val), Type{Kind: TypeI64}, nil
		}
		val, typ, err := c.lowerExpr(expr.X)
		if err != nil {
			return nil, Type{}, err
		}
		return ir.Op(ir.OpSub, ir.Int64(0), val), typ, nil
	default:
		return nil, Type{}, fmt.Errorf("rtg: unsupported unary operator %s", expr.Op)
	}
}

func (c *Compiler) lowerIndex(expr *ast.IndexExpr) (ir.Fragment, Type, error) {
	// Get pointer base
	base, ok := expr.X.(*ast.Ident)
	if !ok {
		return nil, Type{}, fmt.Errorf("rtg: index expression base must be an identifier")
	}

	typ, ok := c.scope.lookup(base.Name)
	if !ok {
		return nil, Type{}, fmt.Errorf("rtg: unknown identifier %q", base.Name)
	}

	// Get index/offset
	offset, err := c.evalInt(expr.Index)
	if err != nil {
		return nil, Type{}, fmt.Errorf("rtg: index must be a constant integer: %w", err)
	}

	// Handle buffer indexing: buf[offset] returns a memory reference
	if typ.Kind == TypeBuffer {
		buf := c.findBuffer(base.Name)
		if buf == nil {
			return nil, Type{}, fmt.Errorf("rtg: internal error: buffer %q not found", base.Name)
		}
		return bufferMemPlaceholder{slotID: buf.slotID, disp: offset, width: 64}, Type{Kind: TypeI64}, nil
	}

	// Handle pointer indexing
	if typ.Kind != TypeUintptr && typ.Kind != TypeI64 {
		return nil, Type{}, fmt.Errorf("rtg: %s is not a pointer or buffer", base.Name)
	}

	mem := ir.Var(base.Name).Mem()
	if offset != 0 {
		mem = mem.WithDisp(ir.Int64(offset)).(ir.MemVar)
	}

	return mem, Type{Kind: TypeI64}, nil
}

func (c *Compiler) lowerBinary(expr *ast.BinaryExpr) (ir.Fragment, Type, error) {
	// Try constant folding first - if both operands are constants, evaluate at compile time
	if val, err := c.evalInt(expr); err == nil {
		return ir.Int64(val), Type{Kind: TypeI64}, nil
	}

	left, lType, err := c.lowerExpr(expr.X)
	if err != nil {
		return nil, Type{}, err
	}
	right, rType, err := c.lowerExpr(expr.Y)
	if err != nil {
		return nil, Type{}, err
	}
	if !typesCompatible(lType, rType) {
		return nil, Type{}, fmt.Errorf("rtg: type mismatch in binary expression (%s vs %s)", lType, rType)
	}

	op, ok := binaryOps[expr.Op]
	if !ok {
		return nil, Type{}, fmt.Errorf("rtg: unsupported binary operator %s", expr.Op)
	}

	return ir.Op(op, left, right), lType, nil
}

func (c *Compiler) lowerCondition(expr ast.Expr) (ir.Condition, error) {
	switch v := expr.(type) {
	case *ast.BinaryExpr:
		kind, ok := comparisonOps[v.Op]
		if !ok {
			return nil, fmt.Errorf("rtg: unsupported comparison %s", v.Op)
		}

		// Try to evaluate string comparisons at compile time (e.g., runtime.GOARCH == "amd64")
		if v.Op == token.EQL || v.Op == token.NEQ {
			leftStr, leftIsStr := c.evalString(v.X)
			rightStr, rightIsStr := c.evalString(v.Y)
			if leftIsStr && rightIsStr {
				// Both sides are compile-time known strings
				equal := leftStr == rightStr
				if v.Op == token.NEQ {
					equal = !equal
				}
				if equal {
					// Always true: compare 1 == 1
					return ir.CompareCondition{Kind: ir.CompareEqual, Left: ir.Int64(1), Right: ir.Int64(1)}, nil
				}
				// Always false: compare 1 == 0
				return ir.CompareCondition{Kind: ir.CompareEqual, Left: ir.Int64(1), Right: ir.Int64(0)}, nil
			}
		}

		left, lType, err := c.lowerExpr(v.X)
		if err != nil {
			return nil, err
		}
		right, rType, err := c.lowerExpr(v.Y)
		if err != nil {
			return nil, err
		}
		if !typesCompatible(lType, rType) {
			return nil, fmt.Errorf("rtg: type mismatch in comparison (%s vs %s)", lType, rType)
		}
		return ir.CompareCondition{Kind: kind, Left: left, Right: right}, nil
	default:
		val, typ, err := c.lowerExpr(expr)
		if err != nil {
			return nil, err
		}
		switch typ.Kind {
		case TypeBool, TypeI64, TypeUintptr:
			return ir.IsNotEqual(val, ir.Int64(0)), nil
		default:
			return nil, fmt.Errorf("rtg: unsupported condition type %s", typ)
		}
	}
}

// evalString attempts to evaluate an expression as a compile-time known string.
// Returns the string value and true if successful, or ("", false) otherwise.
func (c *Compiler) evalString(expr ast.Expr) (string, bool) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			val, err := strconv.Unquote(v.Value)
			if err != nil {
				return "", false
			}
			return val, true
		}
	case *ast.SelectorExpr:
		// Handle runtime.GOARCH
		pkg, ok := v.X.(*ast.Ident)
		if !ok || pkg.Name != "runtime" {
			return "", false
		}
		if v.Sel.Name == "GOARCH" && c.opts.GOARCH != "" {
			return c.opts.GOARCH, true
		}
	}
	return "", false
}

func (c *Compiler) lowerExprCall(call *ast.CallExpr) (ir.Fragment, Type, error) {
	// Get the function name, handling both plain identifiers and runtime.X selectors
	funcName, err := c.resolveFuncName(call.Fun)
	if err != nil {
		return nil, Type{}, err
	}

	switch funcName {
	case "syscall", "Syscall":
		return c.lowerSyscall(call)
	case "load8", "Load8":
		return c.lowerLoad(call, 8)
	case "load16", "Load16":
		return c.lowerLoad(call, 16)
	case "load32", "Load32":
		return c.lowerLoad(call, 32)
	case "load64", "Load64":
		return c.lowerLoad(call, 64)
	case "call", "Call":
		return c.lowerIndirectCall(call)
	case "ifdef", "Ifdef":
		return c.lowerIfdef(call)
	case "config", "Config":
		return c.lowerConfig(call)
	default:
		// Check if it's a call to a user-defined function
		sig, ok := c.funcSigs[funcName]
		if !ok {
			return nil, Type{}, fmt.Errorf("rtg: unknown function %q", funcName)
		}

		// Validate argument count
		if len(call.Args) != len(sig.params) {
			return nil, Type{}, fmt.Errorf("rtg: function %q expects %d arguments, got %d", funcName, len(sig.params), len(call.Args))
		}

		// User-defined functions are called via ir.CallMethod and return int64
		if len(call.Args) == 0 {
			return ir.CallMethod(funcName), Type{Kind: TypeI64}, nil
		}

		// Evaluate arguments
		args := make([]ir.Fragment, len(call.Args))
		for i, arg := range call.Args {
			frag, _, err := c.lowerExpr(arg)
			if err != nil {
				return nil, Type{}, err
			}
			args[i] = frag
		}

		// Generate CallWithArgs using method pointer
		// Return type is int64 (or the function's actual return type)
		retType := sig.returnType
		if retType.Kind == TypeInvalid {
			retType = Type{Kind: TypeI64}
		}
		return ir.CallWithArgs(ir.MethodPointer(funcName), args), retType, nil
	}
}

func (c *Compiler) lowerSyscall(call *ast.CallExpr) (ir.Fragment, Type, error) {
	if len(call.Args) == 0 {
		return nil, Type{}, fmt.Errorf("rtg: syscall requires a number and args")
	}
	num, err := c.evalSyscallNumber(call.Args[0])
	if err != nil {
		return nil, Type{}, fmt.Errorf("rtg: syscall number: %w", err)
	}

	args := make([]any, 0, len(call.Args)-1)
	for _, arg := range call.Args[1:] {
		frag, _, err := c.lowerExpr(arg)
		if err != nil {
			return nil, Type{}, err
		}
		args = append(args, frag)
	}

	return ir.Syscall(num, args...), Type{Kind: TypeI64}, nil
}

func (c *Compiler) lowerGotoCall(call *ast.CallExpr) (ir.Fragment, error) {
	if len(call.Args) != 1 {
		return nil, fmt.Errorf("rtg: gotoLabel expects a single label argument")
	}
	arg, typ, err := c.lowerExpr(call.Args[0])
	if err != nil {
		return nil, err
	}
	if typ.Kind != TypeLabel {
		return nil, fmt.Errorf("rtg: gotoLabel requires a label argument")
	}
	return ir.Goto(arg), nil
}

func (c *Compiler) lowerStore(call *ast.CallExpr, width int) (ir.Fragment, error) {
	if len(call.Args) != 3 {
		return nil, fmt.Errorf("rtg: store%d expects (ptr, offset, value)", width)
	}

	mem, err := c.lowerMemRef(call.Args[0], call.Args[1], width)
	if err != nil {
		return nil, err
	}
	value, _, err := c.lowerExpr(call.Args[2])
	if err != nil {
		return nil, err
	}

	return ir.Assign(mem, value), nil
}

func (c *Compiler) lowerLoad(call *ast.CallExpr, width int) (ir.Fragment, Type, error) {
	if len(call.Args) != 2 {
		return nil, Type{}, fmt.Errorf("rtg: load%d expects (ptr, offset)", width)
	}

	mem, err := c.lowerMemRef(call.Args[0], call.Args[1], width)
	if err != nil {
		return nil, Type{}, err
	}

	return mem, Type{Kind: TypeI64}, nil
}

func (c *Compiler) lowerIndirectCall(call *ast.CallExpr) (ir.Fragment, Type, error) {
	if len(call.Args) != 1 {
		return nil, Type{}, fmt.Errorf("rtg: call expects (target)")
	}

	target, _, err := c.lowerExpr(call.Args[0])
	if err != nil {
		return nil, Type{}, err
	}

	return ir.CallFragment{Target: target}, Type{Kind: TypeI64}, nil
}

// lowerIfdef handles runtime.Ifdef("flag") calls.
// It evaluates the flag at compile time and returns a boolean constant.
// Undefined flags are treated as false.
func (c *Compiler) lowerIfdef(call *ast.CallExpr) (ir.Fragment, Type, error) {
	if len(call.Args) != 1 {
		return nil, Type{}, fmt.Errorf("rtg: Ifdef expects exactly one string argument")
	}

	// Extract the flag name - must be a string literal
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return nil, Type{}, fmt.Errorf("rtg: Ifdef argument must be a string literal")
	}

	// Unquote the string
	flagName, err := strconv.Unquote(lit.Value)
	if err != nil {
		return nil, Type{}, fmt.Errorf("rtg: Ifdef: invalid string literal: %w", err)
	}

	// Look up the flag in compile options
	flagValue := false
	if c.opts.Flags != nil {
		flagValue = c.opts.Flags[flagName]
	}

	// Return compile-time constant
	if flagValue {
		return ir.Int64(1), Type{Kind: TypeBool}, nil
	}
	return ir.Int64(0), Type{Kind: TypeBool}, nil
}

// lowerConfig handles runtime.Config("key") calls.
// It looks up the key in compile-time Config options and returns the value.
// Supported types: string -> TypeString, int64 -> TypeI64
func (c *Compiler) lowerConfig(call *ast.CallExpr) (ir.Fragment, Type, error) {
	if len(call.Args) != 1 {
		return nil, Type{}, fmt.Errorf("rtg: Config expects exactly one string argument")
	}

	// Extract the key - must be a string literal
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return nil, Type{}, fmt.Errorf("rtg: Config argument must be a string literal")
	}

	// Unquote the string
	keyName, err := strconv.Unquote(lit.Value)
	if err != nil {
		return nil, Type{}, fmt.Errorf("rtg: Config: invalid string literal: %w", err)
	}

	// Look up the value in compile options
	if c.opts.Config == nil {
		return nil, Type{}, fmt.Errorf("rtg: Config(%q): no config values provided", keyName)
	}

	value, ok := c.opts.Config[keyName]
	if !ok {
		return nil, Type{}, fmt.Errorf("rtg: Config(%q): key not found", keyName)
	}

	// Return appropriate type based on value
	switch v := value.(type) {
	case string:
		return stringConstant(v), Type{Kind: TypeString}, nil
	case int64:
		return ir.Int64(v), Type{Kind: TypeI64}, nil
	case int:
		return ir.Int64(int64(v)), Type{Kind: TypeI64}, nil
	default:
		return nil, Type{}, fmt.Errorf("rtg: Config(%q): unsupported value type %T", keyName, value)
	}
}

// stringConstant wraps a string value for use in RTG.
// This is a placeholder that gets resolved during code generation.
type stringConstant string

func (s stringConstant) Fragment() {}
func (s stringConstant) String() string {
	return string(s)
}

// lowerEmbedAssign handles multi-value assignment from embed functions:
// ptr, len := runtime.EmbedString("...")
// ptr, len := runtime.EmbedCString("...")
// ptr, len := runtime.EmbedBytes(0x01, 0x02, ...)
func (c *Compiler) lowerEmbedAssign(assign *ast.AssignStmt, call *ast.CallExpr, funcName string) ([]ir.Fragment, error) {
	// Get the two target identifiers
	ptrIdent, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, fmt.Errorf("rtg: embed assignment left-hand side must be identifiers")
	}
	lenIdent, ok := assign.Lhs[1].(*ast.Ident)
	if !ok {
		return nil, fmt.Errorf("rtg: embed assignment left-hand side must be identifiers")
	}

	// Extract the data to embed
	var data []byte
	zeroTerminate := false

	switch funcName {
	case "EmbedString", "EmbedCString":
		if len(call.Args) != 1 {
			return nil, fmt.Errorf("rtg: %s expects exactly one string argument", funcName)
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return nil, fmt.Errorf("rtg: %s argument must be a string literal", funcName)
		}
		str, err := strconv.Unquote(lit.Value)
		if err != nil {
			return nil, fmt.Errorf("rtg: %s: invalid string literal: %w", funcName, err)
		}
		data = []byte(str)
		zeroTerminate = (funcName == "EmbedCString")

	case "EmbedBytes":
		if len(call.Args) == 0 {
			return nil, fmt.Errorf("rtg: EmbedBytes requires at least one byte argument")
		}
		for i, arg := range call.Args {
			val, err := c.evalInt(arg)
			if err != nil {
				return nil, fmt.Errorf("rtg: EmbedBytes argument %d: %w", i, err)
			}
			if val < 0 || val > 255 {
				return nil, fmt.Errorf("rtg: EmbedBytes argument %d out of byte range: %d", i, val)
			}
			data = append(data, byte(val))
		}

	case "EmbedConfigString", "EmbedConfigCString":
		if len(call.Args) != 1 {
			return nil, fmt.Errorf("rtg: %s expects exactly one string argument", funcName)
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return nil, fmt.Errorf("rtg: %s argument must be a string literal", funcName)
		}
		key, err := strconv.Unquote(lit.Value)
		if err != nil {
			return nil, fmt.Errorf("rtg: %s: invalid string literal: %w", funcName, err)
		}
		// Look up the config value
		if c.opts.Config == nil {
			return nil, fmt.Errorf("rtg: %s(%q): no config values provided", funcName, key)
		}
		value, ok := c.opts.Config[key]
		if !ok {
			return nil, fmt.Errorf("rtg: %s(%q): key not found", funcName, key)
		}
		str, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("rtg: %s(%q): value must be a string, got %T", funcName, key, value)
		}
		data = []byte(str)
		zeroTerminate = (funcName == "EmbedConfigCString")

	default:
		return nil, fmt.Errorf("rtg: unknown embed function %q", funcName)
	}

	// Generate unique target variable for the asm code
	c.embedVarCtr++
	target := asm.Variable(2000 + c.embedVarCtr) // Start from 2000 to avoid conflicts

	// Create ir variable names
	ptrVar := ir.Var(ptrIdent.Name)
	lenVar := ir.Var(lenIdent.Name)

	// Define the variables in scope
	switch assign.Tok {
	case token.DEFINE:
		if err := c.scope.define(ptrIdent.Name, Type{Kind: TypeUintptr}); err != nil {
			return nil, err
		}
		if err := c.scope.define(lenIdent.Name, Type{Kind: TypeI64}); err != nil {
			return nil, err
		}
	case token.ASSIGN:
		// Variables should already exist
		if _, ok := c.scope.lookup(ptrIdent.Name); !ok {
			return nil, fmt.Errorf("rtg: assignment to undefined identifier %q", ptrIdent.Name)
		}
		if _, ok := c.scope.lookup(lenIdent.Name); !ok {
			return nil, fmt.Errorf("rtg: assignment to undefined identifier %q", lenIdent.Name)
		}
	default:
		return nil, fmt.Errorf("rtg: unsupported assignment operator %s", assign.Tok)
	}

	// Return the LoadConstantBytesConfig fragment
	return []ir.Fragment{
		ir.LoadConstantBytesConfig(ir.ConstantBytesConfig{
			Target:        target,
			Data:          data,
			ZeroTerminate: zeroTerminate,
			Pointer:       ptrVar,
			Length:        lenVar,
		}),
	}, nil
}

func (c *Compiler) lowerMemRef(ptr ast.Expr, offset ast.Expr, width int) (ir.Fragment, error) {
	// First, check if ptr is a buffer-related expression
	if bufMem, ok := c.tryLowerBufferMemRef(ptr, offset, width); ok {
		return bufMem, nil
	}

	// Fall back to variable-based memory reference
	base, disp, err := c.extractPointer(ptr, offset)
	if err != nil {
		return nil, err
	}

	mem := ir.Var(base).Mem()
	if disp != nil {
		mem = mem.WithDisp(disp).(ir.MemVar)
	}

	switch width {
	case 8:
		return mem.As8(), nil
	case 16:
		return mem.As16(), nil
	case 32:
		return mem.As32(), nil
	case 64:
		return mem, nil
	default:
		return nil, fmt.Errorf("rtg: unsupported memory width %d", width)
	}
}

// tryLowerBufferMemRef tries to lower ptr+offset to a buffer memory reference.
// Returns (fragment, true) if successful, or (nil, false) if ptr is not a buffer expression.
func (c *Compiler) tryLowerBufferMemRef(ptr ast.Expr, offset ast.Expr, width int) (ir.Fragment, bool) {
	// Get the offset value
	offVal, err := c.evalInt(offset)
	if err != nil {
		return nil, false
	}

	// Case 1: &buf[index] - unary address-of on buffer index
	if unary, ok := ptr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		if indexExpr, ok := unary.X.(*ast.IndexExpr); ok {
			if base, ok := indexExpr.X.(*ast.Ident); ok {
				typ, ok := c.scope.lookup(base.Name)
				if ok && typ.Kind == TypeBuffer {
					buf := c.findBuffer(base.Name)
					if buf == nil {
						return nil, false
					}
					indexVal, err := c.evalInt(indexExpr.Index)
					if err != nil {
						return nil, false
					}
					totalDisp := indexVal + offVal
					return bufferMemPlaceholder{slotID: buf.slotID, disp: totalDisp, width: width}, true
				}
			}
		}
	}

	// Case 2: buf - buffer variable used directly as pointer
	if ident, ok := ptr.(*ast.Ident); ok {
		typ, ok := c.scope.lookup(ident.Name)
		if ok && typ.Kind == TypeBuffer {
			buf := c.findBuffer(ident.Name)
			if buf == nil {
				return nil, false
			}
			return bufferMemPlaceholder{slotID: buf.slotID, disp: offVal, width: width}, true
		}
	}

	return nil, false
}

func (c *Compiler) extractPointer(ptr ast.Expr, offset ast.Expr) (string, ir.Fragment, error) {
	base, err := c.pointerBase(ptr)
	if err != nil {
		return "", nil, err
	}
	offVal, err := c.evalInt(offset)
	if err != nil {
		return "", nil, fmt.Errorf("rtg: pointer offset: %w", err)
	}
	var disp ir.Fragment
	if offVal != 0 {
		disp = ir.Int64(offVal)
	}
	return base, disp, nil
}

func (c *Compiler) pointerBase(expr ast.Expr) (string, error) {
	switch v := expr.(type) {
	case *ast.Ident:
		typ, ok := c.scope.lookup(v.Name)
		if !ok {
			return "", fmt.Errorf("rtg: unknown pointer %q", v.Name)
		}
		if typ.Kind != TypeUintptr && typ.Kind != TypeI64 {
			return "", fmt.Errorf("rtg: %s is not a pointer", v.Name)
		}
		return v.Name, nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD && v.Op != token.SUB {
			return "", fmt.Errorf("rtg: pointer arithmetic only supports + or -")
		}
		base, err := c.pointerBase(v.X)
		if err == nil {
			return base, nil
		}
		return c.pointerBase(v.Y)
	default:
		return "", fmt.Errorf("rtg: unsupported pointer expression %T", expr)
	}
}

func (c *Compiler) lowerPrintf(call *ast.CallExpr) ([]ir.Fragment, error) {
	if len(call.Args) == 0 {
		return nil, fmt.Errorf("rtg: printf requires a format string")
	}

	formatLit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || formatLit.Kind != token.STRING {
		return nil, fmt.Errorf("rtg: printf first argument must be a string literal")
	}
	format, err := strconv.Unquote(formatLit.Value)
	if err != nil {
		return nil, fmt.Errorf("rtg: printf format: %w", err)
	}

	args := make([]ir.Fragment, 0, len(call.Args)-1)
	for _, arg := range call.Args[1:] {
		val, _, err := c.lowerExpr(arg)
		if err != nil {
			return nil, err
		}
		args = append(args, val)
	}

	anyArgs := make([]any, len(args))
	for i, a := range args {
		anyArgs[i] = a
	}

	return []ir.Fragment{ir.Printf(format, anyArgs...)}, nil
}

func (c *Compiler) lowerLogKmsg(call *ast.CallExpr) ([]ir.Fragment, error) {
	if len(call.Args) != 1 {
		return nil, fmt.Errorf("rtg: LogKmsg requires exactly one string argument")
	}

	msgLit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || msgLit.Kind != token.STRING {
		return nil, fmt.Errorf("rtg: LogKmsg argument must be a string literal")
	}
	msg, err := strconv.Unquote(msgLit.Value)
	if err != nil {
		return nil, fmt.Errorf("rtg: LogKmsg message: %w", err)
	}

	// Generate unique variable and label names
	c.labelCount++
	id := c.labelCount
	fd := ir.Var(fmt.Sprintf("__kmsg_fd_%d", id))
	doneLabel := ir.Label(fmt.Sprintf("__kmsg_done_%d", id))

	// Generate IR to write to /dev/kmsg
	// This is non-fatal: if /dev/kmsg can't be opened, we just skip the write
	return []ir.Fragment{
		ir.Assign(fd, ir.Syscall(
			defs.SYS_OPENAT,
			ir.Int64(int64(linux.AT_FDCWD)),
			"/dev/kmsg",
			ir.Int64(int64(linux.O_WRONLY)),
			ir.Int64(0),
		)),
		ir.If(ir.IsNegative(fd), ir.Goto(doneLabel)),
		ir.Syscall(defs.SYS_WRITE, fd, msg, ir.Int64(int64(len(msg)))),
		ir.Syscall(defs.SYS_CLOSE, fd),
		ir.DeclareLabel(doneLabel, ir.Block{}),
	}, nil
}

func (c *Compiler) evalInt(expr ast.Expr) (int64, error) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind != token.INT {
			return 0, fmt.Errorf("expected int literal")
		}
		return strconv.ParseInt(v.Value, 0, 64)
	case *ast.ParenExpr:
		return c.evalInt(v.X)
	case *ast.UnaryExpr:
		if v.Op != token.SUB && v.Op != token.ADD {
			return 0, fmt.Errorf("unsupported unary operator in int expression: %s", v.Op)
		}
		val, err := c.evalInt(v.X)
		if err != nil {
			return 0, err
		}
		if v.Op == token.SUB {
			return -val, nil
		}
		return val, nil
	case *ast.Ident:
		// Check for user-defined constants first
		if val, ok := c.constants[v.Name]; ok {
			return val, nil
		}
		// Check for builtin constants
		if val, ok := constantValues[v.Name]; ok {
			return val, nil
		}
		return 0, fmt.Errorf("unknown constant %q", v.Name)
	case *ast.SelectorExpr:
		// Handle runtime.X constants
		pkg, ok := v.X.(*ast.Ident)
		if !ok || pkg.Name != "runtime" {
			return 0, fmt.Errorf("unsupported selector in constant expression")
		}
		name := v.Sel.Name
		// Check builtin constants
		if val, ok := constantValues[name]; ok {
			return val, nil
		}
		// Check syscall names
		if strings.HasPrefix(name, "SYS_") {
			if num, ok := syscallNames[name]; ok {
				return int64(num), nil
			}
		}
		return 0, fmt.Errorf("unknown runtime constant %q", name)
	case *ast.BinaryExpr:
		// Try to evaluate binary expression of constants
		left, err := c.evalInt(v.X)
		if err != nil {
			return 0, err
		}
		right, err := c.evalInt(v.Y)
		if err != nil {
			return 0, err
		}
		switch v.Op {
		case token.ADD:
			return left + right, nil
		case token.SUB:
			return left - right, nil
		case token.MUL:
			return left * right, nil
		case token.QUO:
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return left / right, nil
		case token.AND:
			return left & right, nil
		case token.OR:
			return left | right, nil
		case token.XOR:
			return left ^ right, nil
		case token.SHL:
			return left << uint(right), nil
		case token.SHR:
			return left >> uint(right), nil
		default:
			return 0, fmt.Errorf("unsupported binary operator %s in constant expression", v.Op)
		}
	default:
		return 0, fmt.Errorf("expected int literal")
	}
}

func (c *Compiler) evalSyscallNumber(expr ast.Expr) (defs.Syscall, error) {
	switch v := expr.(type) {
	case *ast.Ident:
		if num, ok := syscallNames[v.Name]; ok {
			return num, nil
		}
		return 0, fmt.Errorf("unknown syscall constant %q", v.Name)
	case *ast.SelectorExpr:
		// Handle runtime.SYS_* selectors
		pkg, ok := v.X.(*ast.Ident)
		if !ok || pkg.Name != "runtime" {
			return 0, fmt.Errorf("unsupported selector in syscall number")
		}
		if num, ok := syscallNames[v.Sel.Name]; ok {
			return num, nil
		}
		return 0, fmt.Errorf("unknown syscall constant %q", v.Sel.Name)
	default:
		val, err := c.evalInt(expr)
		if err != nil {
			return 0, err
		}
		return defs.Syscall(val), nil
	}
}

func typesCompatible(left, right Type) bool {
	if left.Kind == right.Kind {
		return true
	}
	if (left.Kind == TypeUintptr && right.Kind == TypeI64) || (left.Kind == TypeI64 && right.Kind == TypeUintptr) {
		return true
	}
	return false
}

var binaryOps = map[token.Token]ir.OpKind{
	token.ADD: ir.OpAdd,
	token.SUB: ir.OpSub,
	token.MUL: ir.OpMul,
	token.QUO: ir.OpDiv,
	token.SHL: ir.OpShl,
	token.SHR: ir.OpShr,
	token.AND: ir.OpAnd,
	token.OR:  ir.OpOr,
	token.XOR: ir.OpXor,
}

var comparisonOps = map[token.Token]ir.CompareKind{
	token.EQL: ir.CompareEqual,
	token.NEQ: ir.CompareNotEqual,
	token.LSS: ir.CompareLess,
	token.LEQ: ir.CompareLessOrEqual,
	token.GTR: ir.CompareGreater,
	token.GEQ: ir.CompareGreaterOrEqual,
}

var syscallNames = map[string]defs.Syscall{
	"SYS_EXIT":           defs.SYS_EXIT,
	"SYS_EXIT_GROUP":     defs.SYS_EXIT_GROUP,
	"SYS_WRITE":          defs.SYS_WRITE,
	"SYS_READ":           defs.SYS_READ,
	"SYS_OPENAT":         defs.SYS_OPENAT,
	"SYS_CLOSE":          defs.SYS_CLOSE,
	"SYS_MMAP":           defs.SYS_MMAP,
	"SYS_MUNMAP":         defs.SYS_MUNMAP,
	"SYS_MOUNT":          defs.SYS_MOUNT,
	"SYS_MKDIRAT":        defs.SYS_MKDIRAT,
	"SYS_MKNODAT":        defs.SYS_MKNODAT,
	"SYS_CHROOT":         defs.SYS_CHROOT,
	"SYS_CHDIR":          defs.SYS_CHDIR,
	"SYS_SETHOSTNAME":    defs.SYS_SETHOSTNAME,
	"SYS_IOCTL":          defs.SYS_IOCTL,
	"SYS_DUP3":           defs.SYS_DUP3,
	"SYS_SETSID":         defs.SYS_SETSID,
	"SYS_REBOOT":         defs.SYS_REBOOT,
	"SYS_INIT_MODULE":    defs.SYS_INIT_MODULE,
	"SYS_CLOCK_SETTIME":  defs.SYS_CLOCK_SETTIME,
	"SYS_CLOCK_GETTIME":  defs.SYS_CLOCK_GETTIME,
	"SYS_SOCKET":         defs.SYS_SOCKET,
	"SYS_SENDTO":         defs.SYS_SENDTO,
	"SYS_RECVFROM":       defs.SYS_RECVFROM,
	"SYS_EXECVE":         defs.SYS_EXECVE,
	"SYS_CLONE":          defs.SYS_CLONE,
	"SYS_WAIT4":          defs.SYS_WAIT4,
	"SYS_MPROTECT":       defs.SYS_MPROTECT,
	"SYS_GETPID":         defs.SYS_GETPID,
	"SYS_PIVOT_ROOT":     defs.SYS_PIVOT_ROOT,
	"SYS_UMOUNT2":        defs.SYS_UMOUNT2,
	"SYS_UNLINKAT":       defs.SYS_UNLINKAT,
	"SYS_SYMLINKAT":      defs.SYS_SYMLINKAT,
	"SYS_CONNECT":        defs.SYS_CONNECT,
	"SYS_BIND":           defs.SYS_BIND,
	"SYS_LISTEN":         defs.SYS_LISTEN,
	"SYS_ACCEPT":         defs.SYS_ACCEPT,
	"SYS_SHUTDOWN":       defs.SYS_SHUTDOWN,
	"SYS_SETSOCKOPT":     defs.SYS_SETSOCKOPT,
	"SYS_GETSOCKOPT":     defs.SYS_GETSOCKOPT,
	"SYS_SENDMSG":        defs.SYS_SENDMSG,
	"SYS_RECVMSG":        defs.SYS_RECVMSG,
}

var constantValues = map[string]int64{
	// File descriptor constants
	"AT_FDCWD": int64(linux.AT_FDCWD),

	// File mode constants
	"S_IFCHR": int64(linux.S_IFCHR),

	// File open flags
	"O_RDONLY": int64(linux.O_RDONLY),
	"O_WRONLY": int64(linux.O_WRONLY),
	"O_RDWR":   int64(linux.O_RDWR),
	"O_CREAT":  int64(linux.O_CREAT),
	"O_TRUNC":  int64(linux.O_TRUNC),
	"O_SYNC":   int64(linux.O_SYNC),

	// Memory protection flags
	"PROT_READ":  int64(linux.PROT_READ),
	"PROT_WRITE": int64(linux.PROT_WRITE),
	"PROT_EXEC":  int64(linux.PROT_EXEC),

	// Memory map flags
	"MAP_SHARED":    int64(linux.MAP_SHARED),
	"MAP_PRIVATE":   int64(linux.MAP_PRIVATE),
	"MAP_ANONYMOUS": int64(linux.MAP_ANONYMOUS),

	// Error numbers (as negative values for syscall returns)
	"EBUSY":  -int64(linux.EBUSY),
	"EPERM":  -int64(linux.EPERM),
	"EEXIST": -int64(linux.EEXIST),
	"EPIPE":  -int64(linux.EPIPE),

	// Reboot magic numbers
	"LINUX_REBOOT_MAGIC1":        int64(linux.LINUX_REBOOT_MAGIC1),
	"LINUX_REBOOT_MAGIC2":        int64(linux.LINUX_REBOOT_MAGIC2),
	"LINUX_REBOOT_CMD_RESTART":   int64(linux.LINUX_REBOOT_CMD_RESTART),
	"LINUX_REBOOT_CMD_POWER_OFF": int64(linux.LINUX_REBOOT_CMD_POWER_OFF),

	// TTY ioctl
	"TIOCSCTTY": int64(linux.TIOCSCTTY),

	// Clock constants
	"CLOCK_REALTIME": int64(linux.CLOCK_REALTIME),

	// Network constants
	"AF_INET":       int64(linux.AF_INET),
	"AF_NETLINK":    int64(linux.AF_NETLINK),
	"SOCK_DGRAM":    int64(linux.SOCK_DGRAM),
	"SOCK_RAW":      int64(linux.SOCK_RAW),
	"NETLINK_ROUTE": int64(linux.NETLINK_ROUTE),

	// Mount/unmount flags
	"MNT_DETACH": int64(linux.MNT_DETACH),

	// Unlink flags
	"AT_REMOVEDIR": int64(linux.AT_REMOVEDIR),

	// SIGCHLD for clone
	"SIGCHLD": int64(defs.SIGCHLD),

	// Network interface flags
	"IFF_UP":          int64(linux.IFF_UP),
	"SIOCSIFFLAGS":    int64(linux.SIOCSIFFLAGS),
	"SIOCSIFADDR":     int64(linux.SIOCSIFADDR),
	"SIOCSIFNETMASK":  int64(linux.SIOCSIFNETMASK),
	"SIOCGIFINDEX":    int64(linux.SIOCGIFINDEX),
	"RTM_NEWROUTE":    int64(linux.RTM_NEWROUTE),
	"NLM_F_REQUEST":   int64(linux.NLM_F_REQUEST),
	"NLM_F_CREATE":    int64(linux.NLM_F_CREATE),
	"NLM_F_REPLACE":   int64(linux.NLM_F_REPLACE),
	"NLM_F_ACK":       int64(linux.NLM_F_ACK),
	"RT_TABLE_MAIN":   int64(linux.RT_TABLE_MAIN),
	"RTPROT_BOOT":     int64(linux.RTPROT_BOOT),
	"RT_SCOPE_UNIVERSE": int64(linux.RT_SCOPE_UNIVERSE),
	"RTN_UNICAST":     int64(linux.RTN_UNICAST),
	"RTA_OIF":         int64(linux.RTA_OIF),
	"RTA_GATEWAY":     int64(linux.RTA_GATEWAY),

	// Vsock constants
	"AF_VSOCK":        int64(40),   // AF_VSOCK
	"SOCK_STREAM":     int64(1),    // Stream socket type
	"VMADDR_CID_HOST": int64(2),    // Host CID
	"VMADDR_CID_ANY":  int64(-1),   // Bind to any CID
	"VMADDR_PORT_ANY": int64(-1),   // Bind to any port
}

// FormatErrors joins multiple errors when tests want deterministic output.
func FormatErrors(errs []error) string {
	if len(errs) == 0 {
		return ""
	}
	parts := make([]string, len(errs))
	for i, err := range errs {
		parts[i] = err.Error()
	}
	return strings.Join(parts, "; ")
}
