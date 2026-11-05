package check

import (
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
)

// implType is the type an impl is for. A name in it that is not a type is a parameter of the impl: in "impl Pair<A, B>" A and B
// can be any types.
func (c *checker) implType(t ast.Type, ts *tscope) (Type, []*Param) {
	n, ok := t.(*ast.TypeName)
	if !ok {
		c.errorf(t, "impl", "an impl is for a type with a name")
		return bad, nil
	}

	var params []*Param

	args := make([]ast.Type, len(n.Args))
	copy(args, n.Args)

	for _, a := range n.Args {
		an, ok := a.(*ast.TypeName)
		if !ok || len(an.Path) != 1 || len(an.Args) > 0 {
			continue
		}

		if _, isType := ts.typeNamed(an.Path[0]); isType {
			continue
		}

		if ent, _, _ := c.lookupPath(an.Path); ent == nil {
			p := c.newParam(an.Path[0])
			ts.types[an.Path[0]] = p
			params = append(params, p)
		}
	}

	return c.typeOf(t, ts), params
}

func (c *checker) declareImpl(m *Module, d *ast.Impl) {
	ts := newTScope(nil)

	self, params := c.implType(d.Type, ts)

	con, ok := prune(self).(*Con)
	if !ok {
		if _, isBad := self.(*Bad); !isBad {
			c.errorf(d.Type, "impl", "an impl is for a type with a name, and this is %s", self)
		}

		return
	}

	impl := &Impl{Self: self, Params: params, Methods: map[string]*FnDef{}, Module: m, Decl: d}
	c.implOf[d] = impl

	if d.Trait != nil {
		tn, isName := d.Trait.(*ast.TypeName)
		if !isName {
			c.errorf(d.Trait, "impl", "this is not a trait")
			return
		}

		ent, rest, _ := c.lookupPath(tn.Path)

		tr, isTrait := ent.(*TraitDef)
		if !isTrait || len(rest) > 0 {
			c.errorf(d.Trait, "unknown-name", "%s is not a trait", strings.Join(tn.Path, "::"))
			return
		}

		impl.Trait = tr

		for _, other := range con.Def.Impls {
			if other.Trait == tr && c.sameShape(other.Self, self) {
				c.errorf(d, "duplicate", "%s has the trait %s already", self, tr.Name)
				return
			}
		}
	}

	for _, md := range d.Methods {
		fd := &FnDef{Name: md.Name, Module: m, Decl: md, Pub: md.Pub, Impl: impl}
		c.fnSig(fd, ts, params, self)

		if _, dup := impl.Methods[md.Name]; dup {
			c.errorf(md, "duplicate", "%s is declared twice", md.Name)
			continue
		}

		if impl.Trait == nil {
			if _, dup := con.Def.Methods[md.Name]; dup {
				c.errorf(md, "duplicate", "%s has a method %s already", con.Def.Name, md.Name)
				continue
			}

			con.Def.Methods[md.Name] = fd
		}

		impl.Methods[md.Name] = fd
		impl.List = append(impl.List, fd)
	}

	if impl.Trait != nil {
		con.Def.Impls = append(con.Def.Impls, impl)
		c.conform(impl)
	}
}

// conform checks that an impl has the methods of its trait, with the signatures the trait gives them.
func (c *checker) conform(impl *Impl) {
	tr := impl.Trait

	for name, fd := range impl.Methods {
		if _, ok := tr.Methods[name]; !ok {
			c.errorf(fd.Decl, "trait", "the trait %s has no method %s", tr.Name, name)
		}
	}

	for _, name := range tr.Order {
		tm := tr.Methods[name]

		im, ok := impl.Methods[name]
		if !ok {
			c.errorf(impl.Decl, "trait", "%s is missing the method %s of the trait %s", impl.Self, name, tr.Name)
			continue
		}

		if tm.Type == nil || im.Type == nil {
			continue
		}

		c.conformMethod(impl, tm, im)
	}
}

func (c *checker) conformMethod(impl *Impl, tm, im *FnDef) {
	// the trait's own type parameters stand for the impl's, in the order they are written; Self is the type of the impl
	s := &subst{types: map[*Param]Type{tm.Trait.Self: impl.Self}}

	ownT := tm.TParams[1:]
	implOwn := im.TParams[len(impl.Params):]

	if len(ownT) != len(implOwn) || len(tm.EParams) != len(im.EParams) {
		c.errorf(im.Decl, "trait", "%s has other generic parameters than the trait gives it", im.Name)
		return
	}

	for i, p := range ownT {
		s.types[p] = implOwn[i]
	}

	rename := map[string]string{}
	for i, e := range tm.EParams {
		rename[e] = im.EParams[i]
	}

	want := s.ty(tm.Type).(*Fn)

	if len(want.Params) != len(im.Type.Params) {
		c.errorf(im.Decl, "trait", "%s takes %d parameters, and the trait says %d", im.Name, len(im.Type.Params), len(want.Params))
		return
	}

	for i := range want.Params {
		if !sameType(want.Params[i], im.Type.Params[i]) {
			c.errorf(im.Decl.Params[i], "trait", "parameter %d of %s is %s, and the trait says %s", i+1, im.Name, im.Type.Params[i], want.Params[i])
		}
	}

	if !sameType(want.Ret, im.Type.Ret) {
		c.errorf(im.Decl, "trait", "%s gives %s, and the trait says %s", im.Name, im.Type.Ret, want.Ret)
	}

	// the impl may do less than the trait allows, and not more
	allowed := map[string]bool{}

	for _, n := range tm.Type.Eff.names() {
		if r, ok := rename[n]; ok {
			n = r
		}

		allowed[n] = true
	}

	var extra []string

	for _, n := range im.Type.Eff.names() {
		if !allowed[n] {
			extra = append(extra, n)
		}
	}

	if len(extra) > 0 {
		c.errorf(im.Decl, "effect", "%s has the effects %s, and the trait %s does not allow them in %s", im.Name, join(extra), impl.Trait.Name, tm.Name)
	}
}

// sameType says whether two types are the same, with no variables bound.
func sameType(a, b Type) bool {
	a, b = prune(a), prune(b)

	if a == b {
		return true
	}

	if _, ok := a.(*Bad); ok {
		return true
	}

	if _, ok := b.(*Bad); ok {
		return true
	}

	switch a := a.(type) {
	case *Con:
		b, ok := b.(*Con)
		if !ok || a.Def != b.Def || len(a.Args) != len(b.Args) {
			return false
		}

		for i := range a.Args {
			if !sameType(a.Args[i], b.Args[i]) {
				return false
			}
		}

		return true
	case *Fn:
		b, ok := b.(*Fn)
		if !ok || len(a.Params) != len(b.Params) || strings.Join(a.Eff.names(), ",") != strings.Join(b.Eff.names(), ",") {
			return false
		}

		for i := range a.Params {
			if !sameType(a.Params[i], b.Params[i]) {
				return false
			}
		}

		return sameType(a.Ret, b.Ret)
	}

	return false
}

// instantiate gives a function fresh variables for its parameters, and variables for the effects it takes.
func (c *checker) instantiate(fd *FnDef) (*Fn, *subst) {
	s := &subst{types: map[*Param]Type{}, effs: map[string]*EffVar{}}

	for _, p := range fd.TParams {
		s.types[p] = c.newVar()
	}

	for _, e := range fd.EParams {
		s.effs[e] = c.newEffVar()
	}

	return s.ty(fd.Type).(*Fn), s
}

// implements says whether a type has a trait.
func (c *checker) implements(t Type, tr *TraitDef) bool {
	switch t := prune(t).(type) {
	case *Param:
		for _, b := range t.Bounds {
			if b == tr {
				return true
			}
		}
	case *Con:
		for _, impl := range t.Def.Impls {
			if impl.Trait != tr {
				continue
			}

			mark := c.mark()
			ok := c.unify(c.freshen(impl.Self, impl.Params), t)
			c.rollback(mark)

			if ok {
				return true
			}
		}
	case *Bad:
		return true
	}

	return false
}

// freshen is the type with new variables for the parameters.
func (c *checker) freshen(t Type, params []*Param) Type {
	if len(params) == 0 {
		return t
	}

	s := &subst{types: map[*Param]Type{}}
	for _, p := range params {
		s.types[p] = c.newVar()
	}

	return s.ty(t)
}

// findMethod finds the method of a type, with the impl or the trait it comes from instantiated for it.
func (c *checker) findMethod(t Type, name string) (*FnDef, bool) {
	switch t := prune(t).(type) {
	case *Con:
		if fd, ok := t.Def.Methods[name]; ok {
			return fd, true
		}

		var found *FnDef

		for _, impl := range t.Def.Impls {
			fd, ok := impl.Methods[name]
			if !ok {
				continue
			}

			mark := c.mark()
			fits := c.unify(c.freshen(impl.Self, impl.Params), t)
			c.rollback(mark)

			if fits {
				if found != nil && found != fd {
					return found, true
				}

				found = fd
			}
		}

		return found, found != nil
	case *Param:
		for _, b := range t.Bounds {
			if fd, ok := b.Methods[name]; ok {
				return fd, true
			}
		}
	}

	return nil, false
}
