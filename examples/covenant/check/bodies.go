package check

import (
	"strings"
	"unicode"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
)

// bodies checks the functions and constants of a module.
func (c *checker) bodies(m *Module) {
	for _, it := range m.File.Items {
		switch d := it.Decl.(type) {
		case *ast.Fn:
			if dd, ok := m.Decls[d.Name]; ok {
				if fd, ok := dd.Entity.(*FnDef); ok && fd.Decl == d {
					c.checkFn(fd)
				}
			}
		case *ast.Impl:
			if impl := c.implOf[d]; impl != nil {
				for _, fd := range impl.List {
					c.checkFn(fd)
				}
			}
		case *ast.Const:
			if dd, ok := m.Decls[d.Name]; ok {
				if cd, ok := dd.Entity.(*ConstDef); ok && cd.Decl == d {
					c.checkConst(cd)
				}
			}
		}
	}
}

func (c *checker) checkConst(cd *ConstDef) {
	c.fn = &fnCtx{ret: c.unit(), allowed: noEffects}
	c.scope = &scope{vars: map[string]*local{}}
	c.ts = nil

	c.expr(cd.Decl.Value, cd.Type)
}

func (c *checker) push() { c.scope = &scope{parent: c.scope, vars: map[string]*local{}} }
func (c *checker) pop()  { c.scope = c.scope.parent }

// declareLocal adds a name to the scope. A name can be declared again in an inner scope, and not in the same one.
func (c *checker) declareLocal(at ast.Node, name string, ty Type, mutable bool) {
	if name == "_" {
		return
	}

	if _, dup := c.scope.vars[name]; dup {
		c.errorf(at, "duplicate", "%s is declared twice in the same block", name)
		return
	}

	c.scope.vars[name] = &local{name: name, ty: ty, mutable: mutable, depth: c.fn.depth}
}

// declareTracked declares a name and notes it, so that the end of the function can say if its type was never worked out.
func (c *checker) declareTracked(at ast.Node, name string, ty Type, mutable bool) {
	c.declareLocal(at, name, ty, mutable)

	if l := c.scope.vars[name]; l != nil {
		c.fn.decls = append(c.fn.decls, declared{at, l})
	}
}

// noUnit says that a name can not be made of something that gives nothing, like an if.
func (c *checker) noUnit(at ast.Node, value ast.Expr, t Type, written bool) {
	if !c.isUnit(t) || written {
		return
	}

	if n, ok := value.(*ast.Name); ok && len(n.Path) == 1 && n.Path[0] == "Unit" {
		return
	}

	c.errorf(at, "no-value", "this has no value to name: it gives Unit")
}

// inferred reports the names whose type was never worked out: an empty list that nothing was put in.
func (c *checker) inferred(f *fnCtx) {
	for _, d := range f.decls {
		if hasVar(d.local.ty) {
			c.errorf(d.at, "cannot-infer", "the type of %s is not known: say what it is, as %s: Type", d.local.name, d.local.name)
		}
	}
}

func hasVar(t Type) bool {
	switch t := prune(t).(type) {
	case *Var:
		return true
	case *Con:
		for _, a := range t.Args {
			if hasVar(a) {
				return true
			}
		}
	case *Fn:
		for _, p := range t.Params {
			if hasVar(p) {
				return true
			}
		}

		return hasVar(t.Ret)
	}

	return false
}

func (c *checker) checkFn(fd *FnDef) {
	d := fd.Decl
	if d.Body == nil || fd.Type == nil {
		return
	}

	saved := c.cur
	c.cur = fd.Module

	defer func() { c.cur = saved }()

	c.fn = &fnCtx{ret: fd.Type.Ret, allowed: fd.Type.Eff}
	c.scope = &scope{vars: map[string]*local{}}
	c.ts = fd.ts

	for i, p := range d.Params {
		if i < len(fd.Type.Params) {
			c.declareLocal(p, p.Name, fd.Type.Params[i], false)
		}
	}

	c.blockBody(d.Body)
	c.inferred(c.fn)

	if !c.isUnit(fd.Type.Ret) && !terminates(d.Body) {
		c.errorSpan(ast.Span{Begin: d.Body.End - 1, End: d.Body.End}, "return-missing", "%s gives a %s, and not every way through it ends in a return", d.Name, fd.Type.Ret)
	}
}

// blockBody checks the promises of a block and then its statements, in a scope of its own.
func (c *checker) blockBody(b *ast.Block) {
	for _, e := range b.Requires {
		c.promise(e)
	}

	for _, e := range b.Examples {
		c.promise(e)
	}

	c.push()

	for _, s := range b.Stmts {
		c.stmt(s)
	}

	c.pop()
}

// promise checks a requires or an example: a condition that does nothing but look.
func (c *checker) promise(e ast.Expr) {
	saved := c.fn
	c.fn = &fnCtx{ret: c.unit(), allowed: noEffects, depth: saved.depth, parent: saved, pure: true}

	c.expr(e, c.boolT())

	c.fn = saved
}

// terminates says whether every way through the block ends in a return.
func terminates(b *ast.Block) bool {
	if len(b.Stmts) == 0 {
		return false
	}

	switch s := b.Stmts[len(b.Stmts)-1].(type) {
	case *ast.Return:
		return true
	case *ast.ExprStmt:
		if i, ok := s.X.(*ast.If); ok {
			return ifTerminates(i)
		}
	}

	return false
}

func ifTerminates(i *ast.If) bool {
	if i.Else == nil || !terminates(i.Then) {
		return false
	}

	switch e := i.Else.(type) {
	case *ast.Block:
		return terminates(e)
	case *ast.If:
		return ifTerminates(e)
	}

	return false
}

func (c *checker) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.Let:
		c.letStmt(s)
	case *ast.Var:
		var want Type
		if s.Type != nil {
			want = c.typeOf(s.Type, c.ts)
		}

		t := c.expr(s.Value, want)
		if want != nil {
			t = want
		}

		c.noUnit(s, s.Value, t, want != nil)
		c.declareTracked(s, s.Name, t, true)
	case *ast.Return:
		if s.Value == nil {
			if !c.unify(c.fn.ret, c.unit()) {
				c.errorf(s, "type-mismatch", "this function gives a %s, and return has no value", prune(c.fn.ret))
			}

			return
		}

		c.expr(s.Value, c.fn.ret)
	case *ast.For:
		c.forStmt(s)
	case *ast.While:
		c.expr(s.Cond, c.boolT())
		c.blockBody(s.Body)
	case *ast.Assign:
		c.assign(s)
	case *ast.ExprStmt:
		t := c.expr(s.X, nil)
		if con, ok := prune(t).(*Con); ok && con.Def.Name == "Result" && con.Def.Module == "prelude" {
			c.errorf(s, "unused-result", "this gives a %s, and it is dropped: use try, match on it, or let _ = ... to say it does not matter", con)
		}
	}
}

func (c *checker) letStmt(s *ast.Let) {
	name := s.Name

	// a name on its own is not a pattern that takes something apart: it is a name for the value
	if pn, ok := s.Pattern.(*ast.PatName); ok && len(pn.Path) == 1 && !pn.HasArgs && lowerName(pn.Path[0]) {
		name = pn.Path[0]
	}

	if s.Pattern == nil || name != "" {
		var want Type
		if s.Type != nil {
			want = c.typeOf(s.Type, c.ts)
		}

		t := c.expr(s.Value, want)
		if want != nil {
			t = want
		}

		c.noUnit(s, s.Value, t, want != nil)
		c.declareTracked(s, name, t, false)

		return
	}

	t := c.expr(s.Value, nil)

	np := c.pattern(s.Pattern, t, func(name string, ty Type, at ast.Node) {
		c.declareLocal(at, name, ty, false)
	})

	if w, ok := c.missing([][]*npat{{np}}, []Type{t}); !ok {
		c.errorf(s.Pattern, "not-exhaustive", "this pattern does not match every %s: %s is left out", t, w)
	}
}

func (c *checker) forStmt(s *ast.For) {
	it := prune(c.expr(s.Iter, nil))

	var elems []Type

	if con, ok := it.(*Con); ok {
		switch con.Def {
		case c.list, c.set:
			elems = []Type{con.Args[0]}
		case c.mapDef:
			elems = []Type{con.Args[0], con.Args[1]}
		}
	}

	if elems == nil {
		if _, isBad := it.(*Bad); !isBad {
			c.errorf(s.Iter, "type-mismatch", "a for goes over a List, a Set or a Map, and this is %s", it)
		}

		elems = make([]Type, len(s.Patterns))
		for i := range elems {
			elems[i] = bad
		}
	}

	if len(s.Patterns) != len(elems) {
		c.errorf(s, "arity", "this for goes over %s, which has %d names per step, and %d are written", it, len(elems), len(s.Patterns))
		return
	}

	c.push()

	for i, p := range s.Patterns {
		np := c.pattern(p, elems[i], func(name string, ty Type, at ast.Node) {
			c.declareLocal(at, name, ty, false)
		})

		if w, ok := c.missing([][]*npat{{np}}, []Type{elems[i]}); !ok {
			c.errorf(p, "not-exhaustive", "this pattern does not match every %s: %s is left out", elems[i], w)
		}
	}

	for _, st := range s.Body.Stmts {
		c.stmt(st)
	}

	c.pop()

	for _, e := range append(append([]ast.Expr(nil), s.Body.Requires...), s.Body.Examples...) {
		c.errorf(e, "promise", "requires and example are for the start of a function")
	}
}

func (c *checker) assign(s *ast.Assign) {
	switch t := s.Target.(type) {
	case *ast.Name:
		if len(t.Path) == 1 {
			l := c.scope.find(t.Path[0])
			if l == nil {
				c.errorf(t, "unknown-name", "there is no variable %s to assign to", t.Path[0])
				c.expr(s.Value, nil)

				return
			}

			switch {
			case !l.mutable:
				c.errorf(t, "immutable", "%s is not a var: declare it with var to assign to it again", l.name)
			case l.depth < c.fn.depth:
				c.errorf(t, "capture", "a closure has a copy of %s and can not assign to it", l.name)
			}

			c.expr(s.Value, l.ty)

			return
		}
	case *ast.Member:
		c.errorf(t, "not-assignable", "fields are not assigned: make a changed copy with %s with { %s: ... }", exprText(t.X), t.Name)
		c.expr(s.Value, nil)

		return
	}

	c.errorf(s.Target, "not-assignable", "this can not be assigned to: only a var can")
	c.expr(s.Value, nil)
}

func exprText(e ast.Expr) string {
	if n, ok := e.(*ast.Name); ok {
		return strings.Join(n.Path, "::")
	}

	return "..."
}

// lowerName says whether a name is one for a variable: it starts with a lower case letter or an underscore.
func lowerName(s string) bool {
	for _, r := range s {
		return unicode.IsLower(r) || r == '_'
	}

	return false
}
