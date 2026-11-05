package run

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
	"github.com/almerlucke/exbana/v3/examples/covenant/check"
)

// protoOf is the bytecode of a function of the program. It is made the first time it is asked for, together with the functions
// that it calls.
func (p *Program) protoOf(fd *check.FnDef) (*proto, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pr := p.shell(fd)

	return pr, p.drain()
}

func (p *Program) shell(fd *check.FnDef) *proto {
	if pr, ok := p.protos[fd]; ok {
		return pr
	}

	pr := &proto{name: qualified(fd), module: fd.Module, nparams: len(fd.Decl.Params)}
	p.protos[fd] = pr
	p.queue = append(p.queue, func() error { return p.compileFn(fd, pr) })

	return pr
}

// drain makes the bytecode of the functions that were asked for, and of those that they ask for.
func (p *Program) drain() error {
	for len(p.queue) > 0 {
		job := p.queue[0]
		p.queue = p.queue[1:]

		if err := job(); err != nil {
			p.queue = nil
			return err
		}
	}

	return nil
}

// constProto is the bytecode that makes the value of a constant.
func (p *Program) constProto(cd *check.ConstDef) (*proto, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pr, ok := p.constProtos[cd]; ok {
		return pr, nil
	}

	pr, err := p.exprProto(cd.Module, "const "+cd.Name, cd.Decl.Value)
	if err != nil {
		return nil, err
	}

	p.constProtos[cd] = pr

	return pr, p.drain()
}

// exprProto is the bytecode of an expression that is on its own: a constant, an example.
func (p *Program) exprProto(m *check.Module, name string, e ast.Expr) (*proto, error) {
	pr := &proto{name: name, module: m}
	c := &compiler{p: p, mod: m}
	c.fs = &funcState{pr: pr, scopes: []map[string]int{{}}, capNames: map[string]int{}}

	c.expr(e)
	c.emit(opRet, 0, 0, e.At().Begin)

	if c.err != nil {
		return nil, c.err
	}

	return pr, nil
}

func (p *Program) compileFn(fd *check.FnDef, pr *proto) error {
	c := &compiler{p: p, mod: fd.Module}
	c.fs = &funcState{pr: pr, scopes: []map[string]int{{}}, capNames: map[string]int{}}

	for _, prm := range fd.Decl.Params {
		c.declare(prm.Name)
	}

	c.requires(fd.Decl.Body, fd.Name)
	c.stmts(fd.Decl.Body.Stmts, true)
	c.emit(opRetUnit, 0, 0, fd.Decl.Body.End-1)

	return c.err
}

type compiler struct {
	p   *Program
	mod *check.Module
	fs  *funcState
	err error
}

// funcState is the function that is being compiled: its names, and the names it takes from the function around it.
type funcState struct {
	pr       *proto
	parent   *funcState
	scopes   []map[string]int
	capNames map[string]int
	capSrc   []capSource
}

// capSource is where the function around a closure gets a value that the closure captures.
type capSource struct {
	fromCapture bool
	idx         int
}

type nameKind int

const (
	notFound nameKind = iota
	isLocal
	isCaptured
)

// resolve finds a name: one of the function, or one that the function takes from the functions around it.
func (fs *funcState) resolve(name string) (nameKind, int) {
	for i := len(fs.scopes) - 1; i >= 0; i-- {
		if slot, ok := fs.scopes[i][name]; ok {
			return isLocal, slot
		}
	}

	if idx, ok := fs.capNames[name]; ok {
		return isCaptured, idx
	}

	if fs.parent == nil {
		return notFound, 0
	}

	kind, idx := fs.parent.resolve(name)
	if kind == notFound {
		return notFound, 0
	}

	n := len(fs.capSrc)
	fs.capSrc = append(fs.capSrc, capSource{kind == isCaptured, idx})
	fs.capNames[name] = n

	return isCaptured, n
}

func (c *compiler) fail(at ast.Node, format string, args ...any) {
	if c.err == nil {
		c.err = fmt.Errorf("compiling: "+format+" (at %d)", append(args, at.At().Begin)...)
	}
}

func (c *compiler) emit(op opcode, a, b int, pos int) int {
	pr := c.fs.pr
	pr.code = append(pr.code, instr{op, int32(a), int32(b)})
	pr.pos = append(pr.pos, int32(pos))

	return len(pr.code) - 1
}

// konst puts a value among the constants.
func (c *compiler) konst(v Value) int {
	pr := c.fs.pr

	switch v.(type) {
	case int64, float64, string, bool:
		for i, x := range pr.consts {
			if x == v {
				return i
			}
		}
	}

	pr.consts = append(pr.consts, v)

	return len(pr.consts) - 1
}

// A label is a place in the code that jumps can be made to before it is known where it is.
type label struct {
	at      int
	patches []int
}

func newLabel() *label { return &label{at: -1} }

func (c *compiler) jump(op opcode, l *label, pos int) {
	i := c.emit(op, 0, 0, pos)
	if l.at >= 0 {
		c.fs.pr.code[i].a = int32(l.at)
		return
	}

	l.patches = append(l.patches, i)
}

func (c *compiler) mark(l *label) {
	l.at = len(c.fs.pr.code)

	for _, i := range l.patches {
		c.fs.pr.code[i].a = int32(l.at)
	}
}

func (c *compiler) push() { c.fs.scopes = append(c.fs.scopes, map[string]int{}) }
func (c *compiler) pop()  { c.fs.scopes = c.fs.scopes[:len(c.fs.scopes)-1] }

// declare makes a local variable for a name.
func (c *compiler) declare(name string) int {
	slot := c.fs.pr.nlocals
	c.fs.pr.nlocals++

	if name != "_" {
		c.fs.scopes[len(c.fs.scopes)-1][name] = slot
	}

	return slot
}

// temp makes a local variable that has no name.
func (c *compiler) temp() int { return c.declare("_") }

// requires compiles the promises of a function: a false one is a fault, of the caller.
func (c *compiler) requires(b *ast.Block, fn string) {
	for _, r := range b.Requires {
		pos := r.At().Begin
		ok := newLabel()

		c.expr(r)
		c.jump(opJmpTrue, ok, pos)
		c.emit(opFault, c.konst("requires"), c.konst("requires "+c.text(r)+" is false for the call of "+fn), pos)
		c.mark(ok)
	}
}

func (c *compiler) stmts(list []ast.Stmt, newScope bool) {
	if newScope {
		c.push()
		defer c.pop()
	}

	for _, s := range list {
		c.stmt(s)
	}
}

func (c *compiler) text(n ast.Node) string {
	sp := n.At()
	return c.mod.Source[sp.Begin:sp.End]
}

func simpleName(p ast.Pattern) (string, bool) {
	if pn, ok := p.(*ast.PatName); ok && len(pn.Path) == 1 && !pn.HasArgs && lowerName(pn.Path[0]) {
		return pn.Path[0], true
	}

	return "", false
}

func (c *compiler) stmt(s ast.Stmt) {
	pos := s.At().Begin

	switch s := s.(type) {
	case *ast.Let:
		c.expr(s.Value)

		name := s.Name
		if n, ok := simpleName(s.Pattern); ok {
			name = n
		}

		if s.Pattern == nil || name != "" {
			c.emit(opStore, c.declare(name), 0, pos)
			return
		}

		t := c.temp()
		c.emit(opStore, t, 0, pos)
		c.irrefutable(s.Pattern, t)
	case *ast.Var:
		c.expr(s.Value)
		c.emit(opStore, c.declare(s.Name), 0, pos)
	case *ast.Return:
		if s.Value == nil {
			c.emit(opUnit, 0, 0, pos)
		} else {
			c.expr(s.Value)
		}

		c.emit(opRet, 0, 0, pos)
	case *ast.Assign:
		c.expr(s.Value)

		n, ok := s.Target.(*ast.Name)
		if !ok || len(n.Path) != 1 {
			c.fail(s, "an assignment to something that is not a name")
			return
		}

		kind, idx := c.fs.resolve(n.Path[0])
		if kind != isLocal {
			c.fail(s, "an assignment to %s, which is not a variable of this function", n.Path[0])
			return
		}

		c.emit(opStore, idx, 0, pos)
	case *ast.ExprStmt:
		// an if gives nothing, so there is nothing to throw away
		if i, ok := s.X.(*ast.If); ok {
			c.ifExpr(i)
			return
		}

		c.expr(s.X)
		c.emit(opPop, 0, 0, pos)
	case *ast.While:
		top, end := newLabel(), newLabel()
		c.mark(top)
		c.expr(s.Cond)
		c.jump(opJmpFalse, end, pos)
		c.stmts(s.Body.Stmts, true)
		c.jump(opJmp, top, pos)
		c.mark(end)
	case *ast.For:
		c.forLoop(s)
	}
}

func (c *compiler) forLoop(s *ast.For) {
	pos := s.At().Begin

	c.expr(s.Iter)
	c.emit(opIter, 0, 0, pos)

	c.push()
	defer c.pop()

	top, done := newLabel(), newLabel()
	c.mark(top)

	n := len(s.Patterns)
	c.jump(opIterNext, done, pos)
	c.fs.pr.code[len(c.fs.pr.code)-1].b = int32(n)

	// the values are on the stack, the last on top
	temps := make([]int, n)

	for i := n - 1; i >= 0; i-- {
		temps[i] = c.temp()
		c.emit(opStore, temps[i], 0, pos)
	}

	for i, p := range s.Patterns {
		c.irrefutable(p, temps[i])
	}

	c.stmts(s.Body.Stmts, true)
	c.jump(opJmp, top, pos)
	c.mark(done)
}

// irrefutable binds a pattern that has to match, as in a let or a for: if it does not, which the checker has shown it can not,
// it is a fault.
func (c *compiler) irrefutable(p ast.Pattern, slot int) {
	if _, ok := simpleName(p); ok {
		c.pattern(p, slot, nil)
		return
	}

	if _, ok := p.(*ast.PatWild); ok {
		return
	}

	ok, fail := newLabel(), newLabel()

	c.pattern(p, slot, fail)
	c.jump(opJmp, ok, p.At().Begin)
	c.mark(fail)
	c.emit(opNoMatch, slot, 0, p.At().Begin)
	c.mark(ok)
}

// pattern compiles the test of a pattern against the value in a local variable, and the binding of its names. If it does not
// match, it jumps to fail.
func (c *compiler) pattern(p ast.Pattern, slot int, fail *label) {
	pos := p.At().Begin

	switch p := p.(type) {
	case *ast.PatWild:
	case *ast.PatLit:
		v, err := literalValue(p.Lit)
		if err != nil {
			c.fail(p, "%v", err)
			return
		}

		c.emit(opLoad, slot, 0, pos)
		c.emit(opConst, c.konst(v), 0, pos)
		c.emit(opEq, 0, 0, pos)
		c.jump(opJmpFalse, fail, pos)
	case *ast.PatName:
		if name, ok := simpleName(p); ok {
			c.emit(opLoad, slot, 0, pos)
			c.emit(opStore, c.declare(name), 0, pos)

			return
		}

		ref := c.p.res.Refs[p]

		switch ent := ref.Entity.(type) {
		case *check.Variant:
			c.emit(opLoad, slot, 0, pos)
			c.emit(opIsVariant, c.konst(ent), 0, pos)
		case *check.Def:
			c.emit(opLoad, slot, 0, pos)
			c.emit(opIsStruct, c.konst(ent), 0, pos)
		default:
			c.fail(p, "a pattern that is not for a variant or a struct")
			return
		}

		c.jump(opJmpFalse, fail, pos)

		for i, a := range p.Args {
			if _, wild := a.(*ast.PatWild); wild {
				continue
			}

			sub := c.temp()

			c.emit(opLoad, slot, 0, a.At().Begin)
			c.emit(opFieldN, i, 0, a.At().Begin)
			c.emit(opStore, sub, 0, a.At().Begin)
			c.pattern(a, sub, fail)
		}
	}
}

func literalValue(e *ast.Lit) (Value, error) {
	switch e.Kind {
	case ast.LitInt:
		n, err := strconv.ParseInt(strings.ReplaceAll(e.Text, "_", ""), 0, 64)
		if err != nil {
			return nil, fmt.Errorf("%s does not fit in an Int", e.Text)
		}

		return n, nil
	case ast.LitFloat:
		f, err := strconv.ParseFloat(strings.ReplaceAll(e.Text, "_", ""), 64)
		if err != nil {
			return nil, fmt.Errorf("%s does not fit in a Float", e.Text)
		}

		return f, nil
	case ast.LitText:
		return unquote(e.Text), nil
	}

	return e.Text == "true", nil
}

func (c *compiler) expr(e ast.Expr) {
	pos := e.At().Begin

	switch e := e.(type) {
	case *ast.Lit:
		v, err := literalValue(e)
		if err != nil {
			c.fail(e, "%v", err)
			return
		}

		c.emit(opConst, c.konst(v), 0, pos)
	case *ast.Name:
		c.name(e)
	case *ast.List:
		for _, x := range e.Elems {
			c.expr(x)
		}

		c.emit(opMkList, len(e.Elems), 0, pos)
	case *ast.Map:
		for _, en := range e.Entries {
			c.expr(en.Key)
			c.expr(en.Value)
		}

		c.emit(opMkMap, len(e.Entries), 0, pos)
	case *ast.Call:
		c.call(e)
	case *ast.Member:
		c.expr(e.X)
		c.emit(opField, c.konst(e.Name), 0, pos)
	case *ast.Update:
		c.expr(e.X)

		names := make([]string, len(e.Fields))

		for i, f := range e.Fields {
			names[i] = f.Name

			c.expr(f.Value)
		}

		c.emit(opUpdate, c.konst(names), len(names), pos)
	case *ast.Unary:
		c.expr(e.X)

		switch e.Op {
		case "not":
			c.emit(opNot, 0, 0, pos)
		case "-":
			c.emit(opNeg, 0, 0, pos)
		default:
			c.emit(opTry, 0, 0, pos)
		}
	case *ast.Binary:
		c.binary(e)
	case *ast.Closure:
		c.closure(e)
	case *ast.Match:
		c.match(e)
	case *ast.If:
		c.ifExpr(e)
		c.emit(opUnit, 0, 0, pos)
	default:
		c.fail(e, "an expression the compiler does not know")
	}
}

var binaryOps = map[string]opcode{
	"+": opAdd, "-": opSub, "*": opMul, "/": opDiv, "%": opRem, "<": opLt, "<=": opLe, ">": opGt, ">=": opGe, "==": opEq, "!=": opNe,
}

func (c *compiler) binary(e *ast.Binary) {
	pos := e.At().Begin

	switch e.Op {
	case "and", "or":
		end := newLabel()
		op := opJmpFalseK

		if e.Op == "or" {
			op = opJmpTrueK
		}

		c.expr(e.L)
		c.jump(op, end, pos)
		c.expr(e.R)
		c.mark(end)

		return
	}

	c.expr(e.L)
	c.expr(e.R)
	c.emit(binaryOps[e.Op], 0, 0, pos)
}

func (c *compiler) name(e *ast.Name) {
	pos := e.At().Begin

	if ref, ok := c.p.res.Refs[e]; ok {
		c.refValue(e, ref)
		return
	}

	if len(e.Path) == 1 {
		switch kind, idx := c.fs.resolve(e.Path[0]); kind {
		case isLocal:
			c.emit(opLoad, idx, 0, pos)
			return
		case isCaptured:
			c.emit(opLoadCap, idx, 0, pos)
			return
		}

		if e.Path[0] == "Unit" {
			c.emit(opUnit, 0, 0, pos)
			return
		}
	}

	c.fail(e, "an unbound name %s", strings.Join(e.Path, "::"))
}

func (c *compiler) refValue(e *ast.Name, ref check.Ref) {
	pos := e.At().Begin

	switch ent := ref.Entity.(type) {
	case *check.FnDef:
		c.emit(opConst, c.konst(&Func{name: ent.Name, def: ent}), 0, pos)
	case *check.ConstDef:
		c.emit(opLoadConst, c.konst(ent), 0, pos)
	case *check.VariantRef:
		c.emit(opConst, c.konst(&Enum{ent.Variant.Enum, ent.Variant, nil}), 0, pos)
	case *check.Def:
		if len(ref.Rest) == 1 {
			if v := variantOf(ent, ref.Rest[0]); v != nil {
				c.emit(opConst, c.konst(&Enum{ent, v, nil}), 0, pos)
				return
			}

			if fd, ok := ent.Methods[ref.Rest[0]]; ok {
				c.emit(opConst, c.konst(&Func{name: fd.Name, def: fd}), 0, pos)
				return
			}
		}

		c.fail(e, "a name that is not a value")
	default:
		c.fail(e, "a name that is not a value")
	}
}

// callee is what an instruction calls: the bytecode of a function that has a body, or the function itself if it does not.
func (c *compiler) callee(fd *check.FnDef) int {
	if fd.Decl.Body == nil {
		return c.konst(fd)
	}

	return c.konst(c.p.shell(fd))
}

func (c *compiler) call(e *ast.Call) {
	pos := e.At().Begin

	args := func() {
		for _, a := range e.Args {
			c.expr(a.Value)
		}
	}

	switch f := e.Fun.(type) {
	case *ast.Member:
		c.expr(f.X)
		args()
		c.emit(opCallMethod, c.konst(f.Name), len(e.Args)+1, pos)

		return
	case *ast.Name:
		if ref, ok := c.p.res.Refs[f]; ok {
			switch ent := ref.Entity.(type) {
			case *check.FnDef:
				if len(ref.Rest) == 0 {
					args()
					c.emit(opCallDef, c.callee(ent), len(e.Args), pos)

					return
				}
			case *check.VariantRef:
				args()
				c.emit(opMkVariant, c.konst(ent.Variant), c.order(e, ent.Variant.Fields), pos)

				return
			case *check.Def:
				switch {
				case len(ref.Rest) == 0:
					args()
					c.emit(opMkStruct, c.konst(ent), c.order(e, ent.Fields), pos)
				case variantOf(ent, ref.Rest[0]) != nil:
					v := variantOf(ent, ref.Rest[0])

					args()
					c.emit(opMkVariant, c.konst(v), c.order(e, v.Fields), pos)
				default:
					args()
					c.emit(opCallDef, c.callee(ent.Methods[ref.Rest[0]]), len(e.Args), pos)
				}

				return
			}
		}
	}

	c.expr(e.Fun)
	args()
	c.emit(opCall, len(e.Args), 0, pos)
}

// order is where the arguments of a constructor go among its fields: the constants have the order, or it is -1 if the arguments
// are in the order of the fields.
func (c *compiler) order(e *ast.Call, fields []*check.FieldInfo) int {
	perm := make([]int, len(e.Args))
	identity := len(e.Args) == len(fields)

	for i, a := range e.Args {
		idx := i

		if a.Label != "" {
			for j, f := range fields {
				if f.Name == a.Label {
					idx = j
				}
			}
		}

		perm[i] = idx

		if idx != i {
			identity = false
		}
	}

	if identity {
		return -1
	}

	return c.konst(perm)
}

func (c *compiler) closure(e *ast.Closure) {
	pos := e.At().Begin
	outer := c.fs

	pr := &proto{name: "closure", module: c.mod, nparams: len(e.Params)}
	fs := &funcState{pr: pr, parent: outer, scopes: []map[string]int{{}}, capNames: map[string]int{}}
	c.fs = fs

	for _, p := range e.Params {
		c.declare(p.Name)
	}

	if e.Body == nil {
		c.expr(e.Value)
		c.emit(opRet, 0, 0, pos)
	} else {
		c.requires(e.Body, "closure")
		c.stmts(e.Body.Stmts, true)
		c.emit(opRetUnit, 0, 0, e.Body.End-1)
	}

	c.fs = outer
	pr.ncaps = len(fs.capSrc)

	outer.pr.protos = append(outer.pr.protos, pr)

	for _, src := range fs.capSrc {
		if src.fromCapture {
			c.emit(opLoadCap, src.idx, 0, pos)
		} else {
			c.emit(opLoad, src.idx, 0, pos)
		}
	}

	c.emit(opClosure, len(outer.pr.protos)-1, len(fs.capSrc), pos)
}

func (c *compiler) match(e *ast.Match) {
	pos := e.At().Begin

	c.expr(e.X)

	scrutinee := c.temp()
	c.emit(opStore, scrutinee, 0, pos)

	end := newLabel()

	for _, arm := range e.Arms {
		next := newLabel()

		c.push()
		c.pattern(arm.Pattern, scrutinee, next)
		c.expr(arm.Value)
		c.pop()
		c.jump(opJmp, end, arm.At().Begin)
		c.mark(next)
	}

	c.emit(opNoMatch, scrutinee, 0, pos)
	c.mark(end)
}

func (c *compiler) ifExpr(e *ast.If) {
	pos := e.At().Begin

	els, end := newLabel(), newLabel()

	if e.Else == nil {
		c.expr(e.Cond)
		c.jump(opJmpFalse, end, pos)
		c.stmts(e.Then.Stmts, true)
		c.mark(end)

		return
	}

	c.expr(e.Cond)
	c.jump(opJmpFalse, els, pos)
	c.stmts(e.Then.Stmts, true)
	c.jump(opJmp, end, pos)
	c.mark(els)

	switch el := e.Else.(type) {
	case *ast.Block:
		c.stmts(el.Stmts, true)
	case *ast.If:
		c.ifExpr(el)
	}

	c.mark(end)
}
