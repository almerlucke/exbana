package check

import (
	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
)

// bind records that a variable stands for a type, so that it can be undone.
func (c *checker) bind(v *Var, t Type) {
	v.link = t
	c.trail = append(c.trail, func() { v.link = nil })
}

// mark and rollback undo what was found out since: a try that does not work leaves nothing behind.
func (c *checker) mark() int { return len(c.trail) }

func (c *checker) rollback(mark int) {
	for i := len(c.trail) - 1; i >= mark; i-- {
		c.trail[i]()
	}

	c.trail = c.trail[:mark]
}

// unify makes got a type that can be used where want is wanted, binding variables as it goes. It says whether it can.
func (c *checker) unify(want, got Type) bool {
	want, got = prune(want), prune(got)

	if want == got {
		return true
	}

	if _, ok := want.(*Bad); ok {
		return true
	}

	if _, ok := got.(*Bad); ok {
		return true
	}

	if v, ok := want.(*Var); ok {
		return c.bindVar(v, got)
	}

	if v, ok := got.(*Var); ok {
		return c.bindVar(v, want)
	}

	switch w := want.(type) {
	case *Param:
		return false // the same parameter was caught by ==
	case *Dyn:
		g, ok := got.(*Dyn)

		return ok && g.Trait == w.Trait
	case *Con:
		g, ok := got.(*Con)
		if !ok || g.Def != w.Def || len(g.Args) != len(w.Args) {
			return false
		}

		for i := range w.Args {
			if !c.unify(w.Args[i], g.Args[i]) {
				return false
			}
		}

		return true
	case *Fn:
		g, ok := got.(*Fn)
		if !ok || len(g.Params) != len(w.Params) {
			return false
		}

		for i := range w.Params {
			if !c.unify(w.Params[i], g.Params[i]) {
				return false
			}
		}

		return c.unify(w.Ret, g.Ret) && c.within(g.Eff, w.Eff)
	}

	return false
}

func (c *checker) bindVar(v *Var, t Type) bool {
	if t == Type(v) {
		return true
	}

	if occurs(v, t) {
		return false
	}

	c.bind(v, t)

	return true
}

// within says whether the effects of got are among those of want. A want that has variables takes the effects: that is how a
// generic function gets the effects of the closure it is passed.
func (c *checker) within(got, want *Eff) bool {
	gn := got.names()

	for _, v := range want.Vars {
		for _, n := range gn {
			if !v.names[n] {
				v.names[n] = true
				v, n := v, n
				c.trail = append(c.trail, func() { delete(v.names, n) })
			}
		}
	}

	wn := want.names()
	for _, n := range gn {
		if !slicesContains(wn, n) {
			return false
		}
	}

	return true
}

// expect unifies and says what is wrong if it does not work.
func (c *checker) expect(at ast.Node, want, got Type) bool {
	if c.unify(want, got) {
		return true
	}

	c.mismatch(at, want, got)

	return false
}

func (c *checker) mismatch(at ast.Node, want, got Type) {
	w, g := prune(want), prune(got)

	// a function that does more than the type allows is a different thing to say
	if wf, ok := w.(*Fn); ok {
		if gf, ok := g.(*Fn); ok && len(wf.Params) == len(gf.Params) {
			if extra := minus(gf.Eff.names(), wf.Eff.names()); len(extra) > 0 {
				c.errorf(at, "effect", "this function has the effects %s, and %s does not allow them", join(extra), w)
				return
			}
		}
	}

	// a type that would have to be inside itself
	if v, ok := w.(*Var); ok && occurs(v, g) {
		c.errorf(at, "type-mismatch", "this would make a type that has itself inside it: %s", g)
		return
	}

	if v, ok := g.(*Var); ok && occurs(v, w) {
		c.errorf(at, "type-mismatch", "this would make a type that has itself inside it: %s", w)
		return
	}

	c.errorf(at, "type-mismatch", "expected %s, found %s%s", w, g, c.mixedUp(w, g))
}

// mixedUp is what to try when the types are the ones people mix up.
func (c *checker) mixedUp(want, got Type) string {
	w, g := prune(want), prune(got)

	wc, wok := w.(*Con)
	gc, gok := g.(*Con)

	if wok && gok {
		switch {
		case wc.Def == c.tFloat && gc.Def == c.tInt:
			return " (use .to_float())"
		case wc.Def == c.tInt && gc.Def == c.tFloat:
			return " (Float and Int are not mixed: use .floor() or .round() to get an Int)"
		case gc.Def.Name == "Option" || gc.Def.Name == "Result":
			if len(gc.Args) > 0 && c.sameShape(w, gc.Args[0]) {
				return " (this may not have a value: use try, match, or .unwrap_or)"
			}
		}
	}

	return ""
}

func (c *checker) sameShape(a, b Type) bool {
	mark := c.mark()
	ok := c.unify(a, b)
	c.rollback(mark)

	return ok
}

func minus(a, b []string) []string {
	var out []string

	for _, x := range a {
		if !slicesContains(b, x) {
			out = append(out, x)
		}
	}

	return out
}
