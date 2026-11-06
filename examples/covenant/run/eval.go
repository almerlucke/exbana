package run

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
	"github.com/almerlucke/exbana/v3/examples/covenant/check"
)

// env is the names of a block, and the block around it.
type env struct {
	parent *env
	vars   map[string]Value
}

func newEnv(parent *env) *env { return &env{parent: parent, vars: map[string]Value{}} }

func (e *env) get(name string) (Value, bool) {
	for ; e != nil; e = e.parent {
		if v, ok := e.vars[name]; ok {
			return v, true
		}
	}

	return nil, false
}

func (e *env) define(name string, v Value) {
	if name != "_" {
		e.vars[name] = v
	}
}

func (e *env) set(name string, v Value) bool {
	for ; e != nil; e = e.parent {
		if _, ok := e.vars[name]; ok {
			e.vars[name] = v
			return true
		}
	}

	return false
}

// flatten is a copy of everything that can be seen from here: it is what a closure takes with it. A closure has the values the
// names have when it is made, and nothing it does changes the names of the function around it.
func (e *env) flatten() *env {
	out := newEnv(nil)

	var all []*env
	for s := e; s != nil; s = s.parent {
		all = append(all, s)
	}

	for i := len(all) - 1; i >= 0; i-- {
		for k, v := range all[i].vars {
			out.vars[k] = v
		}
	}

	return out
}

// Func is a function as a value: a function of the program, or a closure with what it captured.
type Func struct {
	name    string
	def     *check.FnDef
	closure *ast.Closure
	env     *env
	module  *check.Module
	proto   *proto  // a closure made by the bytecode machine
	caps    []Value // and the values it captured
}

type machine struct {
	p      *Program
	ctx    context.Context
	steps  int64
	depth  int
	cur    *check.Module
	frames []string
	consts map[*check.ConstDef]Value
}

// returnSig is a return on its way out of a function: a return statement, or a try that found an error.
type returnSig struct{ v Value }

func (*returnSig) Error() string { return "return outside a function" }

func (m *machine) tick() error {
	m.steps++

	if lim := m.p.cfg.MaxSteps; lim > 0 && m.steps > lim {
		return &Limit{Kind: "steps"}
	}

	if m.steps&1023 == 0 && m.ctx != nil {
		if err := m.ctx.Err(); err != nil {
			return &Limit{Kind: "cancelled", Err: err}
		}
	}

	return nil
}

// fault makes the fault, with where it is and the functions that are running.
func (m *machine) fault(at ast.Node, kind, msg string) *Fault {
	f := &Fault{Kind: kind, Msg: msg, Module: m.cur.Name}

	if at != nil {
		f.File, f.Line, f.Col = m.cur.Locate(at.At().Begin)
	}

	for i := len(m.frames) - 1; i >= 0; i-- {
		f.Stack = append(f.Stack, m.frames[i])
	}

	return f
}

// public turns what ended the run into what the caller is told.
func (m *machine) public(err error) error { return publicError(err) }

func publicError(err error) error {
	var f *Fault
	var l *Limit

	switch {
	case errors.As(err, &f), errors.As(err, &l):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return &Limit{Kind: "cancelled", Err: err}
	}

	return err
}

func position(m *check.Module, offset int) (file string, line, col int) { return m.Locate(offset) }

func (m *machine) text(n ast.Node) string {
	sp := n.At()
	return m.cur.Source[sp.Begin:sp.End]
}

func lowerName(s string) bool {
	for _, r := range s {
		return unicode.IsLower(r) || r == '_'
	}

	return false
}

func (m *machine) eval(e ast.Expr, en *env) (Value, error) {
	if err := m.tick(); err != nil {
		return nil, err
	}

	switch e := e.(type) {
	case *ast.Lit:
		return m.literal(e)
	case *ast.Name:
		return m.name(e, en)
	case *ast.List:
		items := make([]Value, len(e.Elems))

		for i, x := range e.Elems {
			v, err := m.eval(x, en)
			if err != nil {
				return nil, err
			}

			items[i] = v
		}

		return &List{&listBase{items}, len(items)}, nil
	case *ast.Map:
		out := NewMap()

		for _, entry := range e.Entries {
			k, err := m.eval(entry.Key, en)
			if err != nil {
				return nil, err
			}

			v, err := m.eval(entry.Value, en)
			if err != nil {
				return nil, err
			}

			out = out.Set(k, v)
		}

		return out, nil
	case *ast.Call:
		return m.call(e, en)
	case *ast.Member:
		x, err := m.eval(e.X, en)
		if err != nil {
			return nil, err
		}

		if s, ok := x.(*Struct); ok {
			if v, ok := s.Field(e.Name); ok {
				return v, nil
			}
		}

		return nil, m.fault(e, "internal", "no field "+e.Name)
	case *ast.Update:
		return m.update(e, en)
	case *ast.Unary:
		return m.unary(e, en)
	case *ast.Binary:
		return m.binary(e, en)
	case *ast.Closure:
		return &Func{name: "closure", closure: e, env: en.flatten(), module: m.cur}, nil
	case *ast.Match:
		return m.match(e, en)
	case *ast.If:
		return m.ifExpr(e, en)
	case *ast.Cast:
		x, err := m.eval(e.X, en)
		if err != nil {
			return nil, err
		}

		return boxed(x, m.p.res.Refs[e].Entity.(*check.TraitDef)), nil
	}

	return nil, m.fault(e, "internal", "an expression the interpreter does not know")
}

func (m *machine) literal(e *ast.Lit) (Value, error) {
	switch e.Kind {
	case ast.LitInt:
		n, err := strconv.ParseInt(strings.ReplaceAll(e.Text, "_", ""), 0, 64)
		if err != nil {
			return nil, m.fault(e, "range", e.Text+" does not fit in an Int")
		}

		return n, nil
	case ast.LitFloat:
		f, err := strconv.ParseFloat(strings.ReplaceAll(e.Text, "_", ""), 64)
		if err != nil {
			return nil, m.fault(e, "range", e.Text+" does not fit in a Float")
		}

		return f, nil
	case ast.LitText:
		return unquote(e.Text), nil
	}

	return e.Text == "true", nil
}

// unquote is the text that a text literal stands for: without the quotes, and with the escapes taken out.
func unquote(lit string) string {
	body := lit[1 : len(lit)-1]
	if !strings.Contains(body, `\`) {
		return body
	}

	var sb strings.Builder

	for i := 0; i < len(body); i++ {
		if body[i] != '\\' || i+1 >= len(body) {
			sb.WriteByte(body[i])
			continue
		}

		i++

		switch body[i] {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		default:
			sb.WriteByte(body[i])
		}
	}

	return sb.String()
}

func (m *machine) name(e *ast.Name, en *env) (Value, error) {
	if ref, ok := m.p.res.Refs[e]; ok {
		return m.refValue(e, ref)
	}

	if len(e.Path) == 1 {
		if v, ok := en.get(e.Path[0]); ok {
			return v, nil
		}

		if e.Path[0] == "Unit" {
			return Unit{}, nil
		}
	}

	return nil, m.fault(e, "internal", "unbound name "+strings.Join(e.Path, "::"))
}

func variantOf(d *check.Def, name string) *check.Variant {
	for _, v := range d.Variants {
		if v.Name == name {
			return v
		}
	}

	return nil
}

func (m *machine) refValue(at ast.Node, ref check.Ref) (Value, error) {
	switch ent := ref.Entity.(type) {
	case *check.FnDef:
		return &Func{name: ent.Name, def: ent}, nil
	case *check.ConstDef:
		return m.constant(ent)
	case *check.VariantRef:
		return &Enum{ent.Variant.Enum, ent.Variant, nil}, nil
	case *check.Def:
		if len(ref.Rest) == 1 {
			if v := variantOf(ent, ref.Rest[0]); v != nil {
				return &Enum{ent, v, nil}, nil
			}

			if fd, ok := ent.Methods[ref.Rest[0]]; ok {
				return &Func{name: fd.Name, def: fd}, nil
			}
		}
	}

	return nil, m.fault(at, "internal", "a name that is not a value")
}

func (m *machine) constant(c *check.ConstDef) (Value, error) {
	if v, ok := m.consts[c]; ok {
		return v, nil
	}

	saved := m.cur
	m.cur = c.Module

	v, err := m.eval(c.Decl.Value, newEnv(nil))

	m.cur = saved

	if err != nil {
		return nil, err
	}

	if m.consts == nil {
		m.consts = map[*check.ConstDef]Value{}
	}

	m.consts[c] = v

	return v, nil
}

func (m *machine) evalAll(xs []*ast.Arg, en *env) ([]Value, error) {
	out := make([]Value, len(xs))

	for i, a := range xs {
		v, err := m.eval(a.Value, en)
		if err != nil {
			return nil, err
		}

		out[i] = v
	}

	return out, nil
}

func (m *machine) call(e *ast.Call, en *env) (Value, error) {
	switch f := e.Fun.(type) {
	case *ast.Member:
		recv, err := m.eval(f.X, en)
		if err != nil {
			return nil, err
		}

		args, err := m.evalAll(e.Args, en)
		if err != nil {
			return nil, err
		}

		fd, self := m.p.dispatch(recv, f.Name)
		if fd == nil {
			return nil, m.fault(f, "internal", "no method "+f.Name)
		}

		return m.callDef(fd, append([]Value{self}, args...), e)
	case *ast.Name:
		if ref, ok := m.p.res.Refs[f]; ok {
			switch ent := ref.Entity.(type) {
			case *check.FnDef:
				if len(ref.Rest) == 0 {
					return m.callWith(e, en, func(args []Value) (Value, error) { return m.callDef(ent, args, e) })
				}
			case *check.VariantRef:
				return m.construct(e, en, ent.Variant.Enum, nil, ent.Variant)
			case *check.Def:
				switch {
				case len(ref.Rest) == 0:
					return m.construct(e, en, ent, ent.Fields, nil)
				case variantOf(ent, ref.Rest[0]) != nil:
					return m.construct(e, en, ent, nil, variantOf(ent, ref.Rest[0]))
				default:
					fd := ent.Methods[ref.Rest[0]]
					return m.callWith(e, en, func(args []Value) (Value, error) { return m.callDef(fd, args, e) })
				}
			}
		}
	}

	fv, err := m.eval(e.Fun, en)
	if err != nil {
		return nil, err
	}

	fn, ok := fv.(*Func)
	if !ok {
		return nil, m.fault(e, "internal", "a call of something that is not a function")
	}

	return m.callWith(e, en, func(args []Value) (Value, error) { return m.callFunc(fn, args, e) })
}

func (m *machine) callWith(e *ast.Call, en *env, do func([]Value) (Value, error)) (Value, error) {
	args, err := m.evalAll(e.Args, en)
	if err != nil {
		return nil, err
	}

	return do(args)
}

// construct makes a value of a struct (fields) or of a variant, from the arguments by position or by label.
func (m *machine) construct(e *ast.Call, en *env, def *check.Def, fields []*check.FieldInfo, v *check.Variant) (Value, error) {
	if v != nil {
		fields = v.Fields
	}

	vals := make([]Value, len(fields))

	for i, a := range e.Args {
		x, err := m.eval(a.Value, en)
		if err != nil {
			return nil, err
		}

		idx := i

		if a.Label != "" {
			for j, f := range fields {
				if f.Name == a.Label {
					idx = j
				}
			}
		}

		vals[idx] = x
	}

	if v != nil {
		return &Enum{def, v, vals}, nil
	}

	return &Struct{Def: def, Fields: vals}, nil
}

func (m *machine) update(e *ast.Update, en *env) (Value, error) {
	x, err := m.eval(e.X, en)
	if err != nil {
		return nil, err
	}

	s, ok := x.(*Struct)
	if !ok {
		return nil, m.fault(e, "internal", "with on something that is not a struct")
	}

	out := &Struct{Def: s.Def, Fields: append([]Value(nil), s.Fields...), Host: s.Host}

	for _, f := range e.Fields {
		v, err := m.eval(f.Value, en)
		if err != nil {
			return nil, err
		}

		for i, fi := range s.Def.Fields {
			if fi.Name == f.Name {
				out.Fields[i] = v
			}
		}
	}

	return out, nil
}

// callFunc calls a function value.
func (m *machine) callFunc(f *Func, args []Value, at ast.Node) (Value, error) {
	if f.def != nil {
		return m.callDef(f.def, args, at)
	}

	return m.callClosure(f, args)
}

func (m *machine) callDef(fd *check.FnDef, args []Value, at ast.Node) (Value, error) {
	if fd.Decl.Body == nil {
		return m.callNative(fd, args, at)
	}

	back := m.cur

	if err := m.enter(at, fd.Module, qualified(fd)); err != nil {
		return nil, err
	}

	defer m.leave(back)

	en := newEnv(nil)
	for i, p := range fd.Decl.Params {
		en.define(p.Name, args[i])
	}

	if err := m.requires(fd.Decl.Body, en, fd.Name); err != nil {
		return nil, err
	}

	return m.finish(m.stmts(fd.Decl.Body.Stmts, newEnv(en)))
}

func qualified(fd *check.FnDef) string {
	if fd.Owner != nil {
		return fd.Owner.Name + "." + fd.Name
	}

	return fd.Module.Name + "::" + fd.Name
}

// enter starts a call: it counts the depth, and the code that runs is in the module of the function.
func (m *machine) enter(at ast.Node, mod *check.Module, name string) error {
	if m.depth >= m.p.cfg.MaxDepth {
		return m.fault(at, "stack", "calls are nested too deep")
	}

	m.depth++
	m.frames = append(m.frames, name)
	m.cur = mod

	return nil
}

// leave ends a call and goes back to the module of the caller.
func (m *machine) leave(back *check.Module) {
	m.depth--
	m.frames = m.frames[:len(m.frames)-1]
	m.cur = back
}

func (m *machine) callClosure(f *Func, args []Value) (Value, error) {
	back := m.cur

	if err := m.enter(f.closure, f.module, "closure"); err != nil {
		return nil, err
	}

	defer m.leave(back)

	en := newEnv(f.env)
	for i, p := range f.closure.Params {
		en.define(p.Name, args[i])
	}

	if f.closure.Body == nil {
		v, err := m.eval(f.closure.Value, en)
		if rs, ok := err.(*returnSig); ok {
			return rs.v, nil
		}

		return v, err
	}

	if err := m.requires(f.closure.Body, en, "closure"); err != nil {
		return nil, err
	}

	return m.finish(m.stmts(f.closure.Body.Stmts, newEnv(en)))
}

// finish is what a body gave: the value of a return, or Unit if it came to its end.
func (m *machine) finish(err error) (Value, error) {
	if rs, ok := err.(*returnSig); ok {
		return rs.v, nil
	}

	if err != nil {
		return nil, err
	}

	return Unit{}, nil
}

// requires checks the promises a function makes about its arguments; a false one is the fault of the caller.
func (m *machine) requires(b *ast.Block, en *env, fn string) error {
	for _, r := range b.Requires {
		v, err := m.eval(r, en)
		if err != nil {
			return err
		}

		if v != true {
			return m.fault(r, "requires", "requires "+m.text(r)+" is false for the call of "+fn)
		}
	}

	return nil
}

func (m *machine) stmts(list []ast.Stmt, en *env) error {
	for _, s := range list {
		if err := m.exec(s, en); err != nil {
			return err
		}
	}

	return nil
}

func (m *machine) exec(s ast.Stmt, en *env) error {
	if err := m.tick(); err != nil {
		return err
	}

	switch s := s.(type) {
	case *ast.Let:
		v, err := m.eval(s.Value, en)
		if err != nil {
			return err
		}

		name := s.Name
		if pn, ok := s.Pattern.(*ast.PatName); ok && len(pn.Path) == 1 && !pn.HasArgs && lowerName(pn.Path[0]) {
			name = pn.Path[0]
		}

		if s.Pattern == nil || name != "" {
			en.define(name, v)
			return nil
		}

		if !m.matchPat(s.Pattern, v, en) {
			return m.fault(s, "internal", "a let pattern that does not match")
		}
	case *ast.Var:
		v, err := m.eval(s.Value, en)
		if err != nil {
			return err
		}

		en.define(s.Name, v)
	case *ast.Return:
		var v Value = Unit{}

		if s.Value != nil {
			x, err := m.eval(s.Value, en)
			if err != nil {
				return err
			}

			v = x
		}

		return &returnSig{v}
	case *ast.Assign:
		v, err := m.eval(s.Value, en)
		if err != nil {
			return err
		}

		if n, ok := s.Target.(*ast.Name); ok && len(n.Path) == 1 {
			en.set(n.Path[0], v)
		}
	case *ast.ExprStmt:
		_, err := m.eval(s.X, en)
		return err
	case *ast.While:
		for {
			c, err := m.eval(s.Cond, en)
			if err != nil {
				return err
			}

			if c != true {
				return nil
			}

			if err := m.stmts(s.Body.Stmts, newEnv(en)); err != nil {
				return err
			}

			if err := m.tick(); err != nil {
				return err
			}
		}
	case *ast.For:
		return m.forLoop(s, en)
	}

	return nil
}

func (m *machine) forLoop(s *ast.For, en *env) error {
	it, err := m.eval(s.Iter, en)
	if err != nil {
		return err
	}

	body := func(vals ...Value) error {
		inner := newEnv(en)

		for i, p := range s.Patterns {
			if !m.matchPat(p, vals[i], inner) {
				return m.fault(p, "internal", "a for pattern that does not match")
			}
		}

		if err := m.stmts(s.Body.Stmts, inner); err != nil {
			return err
		}

		return m.tick()
	}

	switch it := it.(type) {
	case *List:
		for _, x := range it.Items() {
			if err := body(x); err != nil {
				return err
			}
		}
	case *Set:
		for i := range it.Len() {
			if err := body(it.Item(i)); err != nil {
				return err
			}
		}
	case *Map:
		for i := range it.Len() {
			k, v := it.Entry(i)
			if err := body(k, v); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *machine) ifExpr(e *ast.If, en *env) (Value, error) {
	c, err := m.eval(e.Cond, en)
	if err != nil {
		return nil, err
	}

	switch {
	case c == true:
		return Unit{}, m.stmts(e.Then.Stmts, newEnv(en))
	case e.Else == nil:
		return Unit{}, nil
	}

	switch el := e.Else.(type) {
	case *ast.Block:
		return Unit{}, m.stmts(el.Stmts, newEnv(en))
	case *ast.If:
		return m.ifExpr(el, en)
	}

	return Unit{}, nil
}

func (m *machine) match(e *ast.Match, en *env) (Value, error) {
	x, err := m.eval(e.X, en)
	if err != nil {
		return nil, err
	}

	for _, arm := range e.Arms {
		inner := newEnv(en)
		if m.matchPat(arm.Pattern, x, inner) {
			return m.eval(arm.Value, inner)
		}
	}

	return nil, m.fault(e, "internal", "no case of the match fits "+Format(x))
}

func (m *machine) matchPat(p ast.Pattern, v Value, en *env) bool {
	switch p := p.(type) {
	case *ast.PatWild:
		return true
	case *ast.PatLit:
		lit, _ := m.literal(p.Lit)
		return Equal(lit, v)
	case *ast.PatName:
		if len(p.Path) == 1 && !p.HasArgs && lowerName(p.Path[0]) {
			en.define(p.Path[0], v)
			return true
		}

		ref := m.p.res.Refs[p]

		var fields []Value

		switch ent := ref.Entity.(type) {
		case *check.Variant:
			e, ok := v.(*Enum)
			if !ok || e.Variant != ent {
				return false
			}

			fields = e.Fields
		case *check.Def:
			s, ok := v.(*Struct)
			if !ok || s.Def != ent {
				return false
			}

			fields = s.Fields
		default:
			return false
		}

		for i, a := range p.Args {
			if i >= len(fields) || !m.matchPat(a, fields[i], en) {
				return false
			}
		}

		return true
	}

	return false
}

// boxed is a value known by a trait; one that is already is not wrapped again.
func boxed(v Value, tr *check.TraitDef) Value {
	if o, ok := v.(*Obj); ok && o.Trait == tr {
		return o
	}

	return &Obj{tr, v}
}

// dispatch finds the method to call on a value, and the value to call it on. A value that is known by a trait has the method of
// that trait, of the type that it was made from; any other has the method of its type.
func (p *Program) dispatch(recv Value, name string) (*check.FnDef, Value) {
	o, ok := recv.(*Obj)
	if !ok {
		return p.method(recv, name), recv
	}

	if def := p.defOf(o.V); def != nil {
		for _, impl := range def.Impls {
			if impl.Trait == o.Trait {
				if fd, ok := impl.Methods[name]; ok {
					return fd, o.V
				}
			}
		}
	}

	return nil, recv
}

// method finds the method of a value: the type of the value decides, which is how a method of a trait is found for a value
// whose type is a parameter.
func (p *Program) method(recv Value, name string) *check.FnDef {
	def := p.defOf(recv)
	if def == nil {
		return nil
	}

	if fd, ok := def.Methods[name]; ok {
		return fd
	}

	for _, impl := range def.Impls {
		if fd, ok := impl.Methods[name]; ok {
			return fd
		}
	}

	return nil
}

// defOf is the type a value is a value of.
func (p *Program) defOf(recv Value) *check.Def {
	var def *check.Def

	switch v := recv.(type) {
	case int64:
		def = p.defs["Int"]
	case float64:
		def = p.defs["Float"]
	case string:
		def = p.defs["Text"]
	case bool:
		def = p.defs["Bool"]
	case Unit:
		def = p.defs["Unit"]
	case *List:
		def = p.defs["List"]
	case *Map:
		def = p.defs["Map"]
	case *Set:
		def = p.defs["Set"]
	case *Struct:
		def = v.Def
	case *Enum:
		def = v.Def
	}

	return def
}

func (m *machine) unary(e *ast.Unary, en *env) (Value, error) {
	x, err := m.eval(e.X, en)
	if err != nil {
		return nil, err
	}

	switch e.Op {
	case "not":
		return x != true, nil
	case "-":
		v, kind, msg := negate(x)
		if kind != "" {
			return nil, m.fault(e, kind, msg)
		}

		return v, nil
	case "try":
		en, ok := x.(*Enum)
		if !ok {
			break
		}

		switch en.Variant.Name {
		case "Ok", "Some":
			return en.Fields[0], nil
		}

		return nil, &returnSig{en}
	}

	return nil, m.fault(e, "internal", "an operator on a value it does not fit")
}

func (m *machine) binary(e *ast.Binary, en *env) (Value, error) {
	l, err := m.eval(e.L, en)
	if err != nil {
		return nil, err
	}

	switch e.Op {
	case "and":
		if l != true {
			return false, nil
		}

		return m.eval(e.R, en)
	case "or":
		if l == true {
			return true, nil
		}

		return m.eval(e.R, en)
	}

	r, err := m.eval(e.R, en)
	if err != nil {
		return nil, err
	}

	switch e.Op {
	case "==":
		return Equal(l, r), nil
	case "!=":
		return !Equal(l, r), nil
	}

	v, kind, msg := binaryOp(e.Op, l, r)
	if kind != "" {
		return nil, m.fault(e, kind, msg)
	}

	return v, nil
}
