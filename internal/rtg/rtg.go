package rtg

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
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
)

type Type struct {
	Kind TypeKind
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

// Compiler holds the state for a single source-to-IR lowering.
type Compiler struct {
	fset       *token.FileSet
	file       *ast.File
	scope      *scope
	returnType Type
}

// CompileProgram parses src and lowers it into an ir.Program. The accepted
// language is intentionally small; unsupported constructs return friendly
// errors rather than attempting partial lowering.
func CompileProgram(src string) (*ir.Program, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "input.go", src, parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("rtg: parse: %w", err)
	}

	c := &Compiler{
		fset:  fset,
		file:  file,
		scope: newScope(nil),
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

	methods := make(map[string]ir.Method)
	var entrypoint string

	for _, decl := range c.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			return nil, fmt.Errorf("rtg: only func declarations are supported")
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

func (c *Compiler) lowerFunc(fn *ast.FuncDecl) (ir.Method, error) {
	if fn.Recv != nil {
		return nil, fmt.Errorf("rtg: methods are not supported (%s)", fn.Name.Name)
	}
	if fn.Type.TypeParams != nil && fn.Type.TypeParams.NumFields() > 0 {
		return nil, fmt.Errorf("rtg: type parameters are not supported (%s)", fn.Name.Name)
	}
	if fn.Type.Params != nil && fn.Type.Params.NumFields() > 0 {
		return nil, fmt.Errorf("rtg: parameters are not supported (%s)", fn.Name.Name)
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
	defer func() {
		c.scope = prevScope
		c.returnType = prevRet
	}()

	if fn.Body == nil {
		return nil, fmt.Errorf("rtg: function %q has no body", fn.Name.Name)
	}

	var method ir.Method
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
		return nil, fmt.Errorf("rtg: function %q missing return statement", fn.Name.Name)
	}

	return method, nil
}

func resolveType(expr ast.Expr) (Type, error) {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return Type{}, fmt.Errorf("unsupported type %T", expr)
	}
	switch id.Name {
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
	default:
		return Type{}, fmt.Errorf("unsupported type %q", id.Name)
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
	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil, fmt.Errorf("rtg: only single-value assignments are supported")
	}
	ident, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, fmt.Errorf("rtg: left-hand side must be identifier")
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

func (c *Compiler) lowerReturn(ret *ast.ReturnStmt) ([]ir.Fragment, error) {
	if len(ret.Results) > 1 {
		return nil, fmt.Errorf("rtg: multiple return values are not supported")
	}

	if len(ret.Results) == 0 {
		if c.returnType.Kind != TypeInvalid {
			return nil, fmt.Errorf("rtg: return value required (expected %s)", c.returnType)
		}
		// Void return; terminate the method.
		return []ir.Fragment{ir.Return(ir.Int64(0))}, nil
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
	target, ok := call.Fun.(*ast.Ident)
	if !ok {
		return nil, fmt.Errorf("rtg: unsupported call target %T", call.Fun)
	}
	switch target.Name {
	case "printf":
		return c.lowerPrintf(call)
	case "syscall":
		frag, _, err := c.lowerSyscall(call)
		if err != nil {
			return nil, err
		}
		return []ir.Fragment{frag}, nil
	default:
		return nil, fmt.Errorf("rtg: unsupported call %q", target.Name)
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
		return nil, Type{}, fmt.Errorf("string literals are only allowed in printf calls")
	default:
		return nil, Type{}, fmt.Errorf("unsupported literal %s", lit.Kind.String())
	}
}

func (c *Compiler) lowerIdent(id *ast.Ident) (ir.Fragment, Type, error) {
	if typ, ok := c.scope.lookup(id.Name); ok {
		return ir.Var(id.Name), typ, nil
	}
	switch id.Name {
	case "true":
		return ir.Int64(1), Type{Kind: TypeBool}, nil
	case "false":
		return ir.Int64(0), Type{Kind: TypeBool}, nil
	default:
		return nil, Type{}, fmt.Errorf("rtg: unknown identifier %q", id.Name)
	}
}

func (c *Compiler) lowerBinary(expr *ast.BinaryExpr) (ir.Fragment, Type, error) {
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

func (c *Compiler) lowerExprCall(call *ast.CallExpr) (ir.Fragment, Type, error) {
	target, ok := call.Fun.(*ast.Ident)
	if !ok {
		return nil, Type{}, fmt.Errorf("rtg: unsupported call target %T", call.Fun)
	}
	switch target.Name {
	case "syscall":
		return c.lowerSyscall(call)
	default:
		return nil, Type{}, fmt.Errorf("rtg: unsupported call %q", target.Name)
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
	default:
		val, err := c.evalInt(expr)
		if err != nil {
			return 0, err
		}
		return defs.Syscall(val), nil
	}
}

func typesCompatible(left, right Type) bool {
	return left.Kind == right.Kind
}

var binaryOps = map[token.Token]ir.OpKind{
	token.ADD: ir.OpAdd,
	token.SUB: ir.OpSub,
	token.MUL: ir.OpMul,
	token.QUO: ir.OpDiv,
	token.SHL: ir.OpShl,
	token.SHR: ir.OpShr,
	token.AND: ir.OpAnd,
}

var syscallNames = map[string]defs.Syscall{
	"SYS_EXIT":       defs.SYS_EXIT,
	"SYS_EXIT_GROUP": defs.SYS_EXIT_GROUP,
	"SYS_WRITE":      defs.SYS_WRITE,
	"SYS_READ":       defs.SYS_READ,
	"SYS_OPENAT":     defs.SYS_OPENAT,
	"SYS_CLOSE":      defs.SYS_CLOSE,
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
