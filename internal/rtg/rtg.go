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
	TypeString
	TypeLabel
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
	case TypeString:
		return "string"
	case TypeLabel:
		return "label"
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
	case "string":
		return Type{Kind: TypeString}, nil
	case "label":
		return Type{Kind: TypeLabel}, nil
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
	case *ast.IfStmt:
		return c.lowerIf(s)
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
	case "store32":
		frag, err := c.lowerStore(call, 32)
		if err != nil {
			return nil, err
		}
		return []ir.Fragment{frag}, nil
	case "gotoLabel":
		frag, err := c.lowerGotoCall(call)
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
	case *ast.UnaryExpr:
		return c.lowerUnary(v)
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

func (c *Compiler) lowerUnary(expr *ast.UnaryExpr) (ir.Fragment, Type, error) {
	switch expr.Op {
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

func (c *Compiler) lowerCondition(expr ast.Expr) (ir.Condition, error) {
	switch v := expr.(type) {
	case *ast.BinaryExpr:
		kind, ok := comparisonOps[v.Op]
		if !ok {
			return nil, fmt.Errorf("rtg: unsupported comparison %s", v.Op)
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

func (c *Compiler) lowerMemRef(ptr ast.Expr, offset ast.Expr, width int) (ir.Fragment, error) {
	base, disp, err := c.extractPointer(ptr, offset)
	if err != nil {
		return nil, err
	}

	mem := ir.Var(base).Mem()
	if disp != nil {
		mem = mem.WithDisp(disp).(ir.MemVar)
	}

	switch width {
	case 32:
		return mem.As32(), nil
	default:
		return nil, fmt.Errorf("rtg: unsupported store width %d", width)
	}
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
	"SYS_EXIT":        defs.SYS_EXIT,
	"SYS_EXIT_GROUP":  defs.SYS_EXIT_GROUP,
	"SYS_WRITE":       defs.SYS_WRITE,
	"SYS_READ":        defs.SYS_READ,
	"SYS_OPENAT":      defs.SYS_OPENAT,
	"SYS_CLOSE":       defs.SYS_CLOSE,
	"SYS_MMAP":        defs.SYS_MMAP,
	"SYS_MUNMAP":      defs.SYS_MUNMAP,
	"SYS_MOUNT":       defs.SYS_MOUNT,
	"SYS_MKDIRAT":     defs.SYS_MKDIRAT,
	"SYS_CHROOT":      defs.SYS_CHROOT,
	"SYS_CHDIR":       defs.SYS_CHDIR,
	"SYS_SETHOSTNAME": defs.SYS_SETHOSTNAME,
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
