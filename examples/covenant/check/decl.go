package check

import (
	_ "embed"
	"sort"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant"
	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
)

//go:embed prelude.cov
var preludeSource string

// tscope holds the names of type parameters and effect parameters that are in scope.
type tscope struct {
	parent *tscope
	types  map[string]Type
	effs   map[string]string
}

func newTScope(parent *tscope) *tscope {
	return &tscope{parent: parent, types: map[string]Type{}, effs: map[string]string{}}
}

func (s *tscope) typeNamed(name string) (Type, bool) {
	for ; s != nil; s = s.parent {
		if t, ok := s.types[name]; ok {
			return t, true
		}
	}

	return nil, false
}

func (s *tscope) effNamed(name string) (string, bool) {
	for ; s != nil; s = s.parent {
		if e, ok := s.effs[name]; ok {
			return e, true
		}
	}

	return "", false
}

func (c *checker) run(sources map[string]string) {
	c.initPrelude()

	names := make([]string, 0, len(sources)+len(c.cfg.Native))
	native := map[string]bool{}

	for n := range c.cfg.Native {
		names = append(names, n)
		native[n] = true
	}

	for n := range sources {
		if !native[n] {
			names = append(names, n)
		}
	}

	sort.Strings(names)

	for _, name := range names {
		src, ok := sources[name]
		if native[name] {
			src = c.cfg.Native[name]
		} else if !ok {
			continue
		}

		c.load(name, src, native[name])
	}

	order := c.order(names)

	for _, m := range order {
		c.cur = m
		c.declare(m)
	}

	for _, m := range order {
		if m.Native {
			continue
		}

		c.cur = m
		c.bodies(m)
	}
}

// load parses a module. A module that does not parse is left out, and says why.
func (c *checker) load(name, src string, native bool) *Module {
	m := &Module{Name: name, Native: native, Source: src, Decls: map[string]*Decl{}, Imports: map[string]any{}}
	c.mods[name] = m
	c.cur = m

	f, err := covenant.Parse(src)
	if err != nil {
		if e, ok := err.(*covenant.Error); ok {
			c.diags = append(c.diags, Diagnostic{Module: name, Line: e.Line, Col: e.Col, Code: "syntax", Msg: e.Msg})
		} else {
			c.diags = append(c.diags, Diagnostic{Module: name, Code: "syntax", Msg: err.Error()})
		}

		delete(c.mods, name)

		return nil
	}

	m.File = f.AST()
	c.sources[m] = f

	return m
}

// usedModule is the module a use refers to and what is left of the path: the longest start of the path that is a module.
func (c *checker) usedModule(path []string) (*Module, []string) {
	for k := len(path); k > 0; k-- {
		if m, ok := c.mods[strings.Join(path[:k], "::")]; ok {
			return m, path[k:]
		}
	}

	return nil, path
}

// order puts the modules so that a module comes after the ones it uses, and says where there is a cycle.
func (c *checker) order(names []string) []*Module {
	var out []*Module

	state := map[*Module]int{}

	var visit func(m *Module, from []string)

	visit = func(m *Module, from []string) {
		switch state[m] {
		case 2:
			return
		case 1:
			c.cur = m
			c.errorf(nil, "cycle", "modules use each other: %s", strings.Join(append(from, m.Name), " -> "))

			return
		}

		state[m] = 1

		for _, u := range m.File.Uses {
			if dep, _ := c.usedModule(u.Path); dep != nil && dep != m {
				visit(dep, append(from, m.Name))
			}
		}

		state[m] = 2
		out = append(out, m)
	}

	if c.prelude != nil {
		out = append(out, c.prelude)
		state[c.prelude] = 2
	}

	// what the host provides comes first: its signatures are where the effects get their names
	for _, n := range names {
		if m := c.mods[n]; m != nil && m.Native {
			visit(m, nil)
		}
	}

	for _, n := range names {
		if m := c.mods[n]; m != nil {
			visit(m, nil)
		}
	}

	return out
}

func (c *checker) initPrelude() {
	m := &Module{Name: "prelude", Native: true, Source: preludeSource, Decls: map[string]*Decl{}, Imports: map[string]any{}}
	c.mods["prelude"] = m
	c.prelude = m

	prim := func(name string) *Def {
		d := &Def{Name: name, Module: "prelude", Kind: DefPrim, Methods: map[string]*FnDef{}, resolved: true}
		m.Decls[name] = &Decl{Name: name, Pub: true, Entity: d}

		return d
	}

	builtin := func(name string, params ...string) *Def {
		d := &Def{Name: name, Module: "prelude", Kind: DefBuiltin, Methods: map[string]*FnDef{}, resolved: true}
		for _, p := range params {
			d.Params = append(d.Params, c.newParam(p))
		}

		m.Decls[name] = &Decl{Name: name, Pub: true, Entity: d}

		return d
	}

	c.tInt, c.tFloat, c.tText, c.tBool, c.tUnit = prim("Int"), prim("Float"), prim("Text"), prim("Bool"), prim("Unit")
	c.list, c.mapDef, c.set = builtin("List", "T"), builtin("Map", "K", "V"), builtin("Set", "T")

	c.cur = m

	f, err := covenant.Parse(preludeSource)
	if err != nil {
		panic("the prelude does not parse: " + err.Error())
	}

	m.File = f.AST()
	c.sources[m] = f
}

// lookup finds a name from inside a module: what it declares, what it brought in, what the prelude has.
func (c *checker) lookup(m *Module, name string) any {
	if d, ok := m.Decls[name]; ok {
		return d.Entity
	}

	if e, ok := m.Imports[name]; ok {
		return e
	}

	if c.prelude != nil {
		if d, ok := c.prelude.Decls[name]; ok && d.Pub {
			return d.Entity
		}
	}

	return nil
}

// lookupPath follows a path of names through modules. It gives what it reached and the names left over, which are for a type to
// make sense of: a variant or an associated function. private is set if the path went into a module for something that is not
// public.
func (c *checker) lookupPath(path []string) (ent any, rest []string, private string) {
	ent = c.lookup(c.cur, path[0])
	rest = path[1:]

	for {
		mod, ok := ent.(*Module)
		if !ok || len(rest) == 0 {
			break
		}

		d, found := mod.Decls[rest[0]]
		if !found {
			return nil, rest, ""
		}

		if !d.Pub && mod != c.cur {
			return nil, rest, rest[0]
		}

		ent, rest = d.Entity, rest[1:]
	}

	return ent, rest, ""
}

// declare resolves everything a module says about itself, except the bodies of functions.
func (c *checker) declare(m *Module) {
	// the names
	for _, it := range m.File.Items {
		c.collect(m, it)
	}

	c.imports(m)

	// what the names stand for
	for _, d := range sortedDecls(m) {
		switch e := d.Entity.(type) {
		case *Def:
			c.resolveDef(e, m)
		case *AliasDef:
			c.resolveAlias(e)
		case *TraitDef:
			c.resolveTrait(e)
		}
	}

	for _, it := range m.File.Items {
		switch d := it.Decl.(type) {
		case *ast.Fn:
			if fd, ok := m.Decls[d.Name].Entity.(*FnDef); ok && fd.Decl == d {
				c.resolveFn(fd)
			}
		case *ast.Const:
			if cd, ok := m.Decls[d.Name].Entity.(*ConstDef); ok && cd.Decl == d {
				cd.Type = c.typeOf(d.Type, nil)
			}
		case *ast.Group:
			if g, ok := m.Decls[d.Name].Entity.(*GroupDef); ok {
				c.groupNames(g)
			}
		}
	}

	for _, it := range m.File.Items {
		switch d := it.Decl.(type) {
		case *ast.Impl:
			c.declareImpl(m, d)
		case *ast.Trait:
			if m.Native {
				c.nativeTrait(m, d)
			}
		}
	}

	if m == c.prelude {
		c.exportVariants()
	}
}

func sortedDecls(m *Module) []*Decl {
	out := make([]*Decl, 0, len(m.Decls))
	for _, d := range m.Decls {
		out = append(out, d)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Span.Begin < out[j].Span.Begin })

	return out
}

func (c *checker) add(m *Module, name string, pub bool, ent any, sp ast.Span) {
	if _, dup := m.Decls[name]; dup {
		c.errorSpan(sp, "duplicate", "%s is declared twice", name)
		return
	}

	m.Decls[name] = &Decl{name, pub, ent, sp}
}

func (c *checker) collect(m *Module, it *ast.Item) {
	pub := it.Pub || m.Native

	switch d := it.Decl.(type) {
	case *ast.Fn:
		c.add(m, d.Name, pub, &FnDef{Name: d.Name, Module: m, Decl: d, Pub: pub}, d.Span)
	case *ast.Struct:
		def := &Def{Name: d.Name, Module: m.Name, Kind: DefStruct, Methods: map[string]*FnDef{}, pub: pub}
		def.Params = c.genericParams(d.Generics)
		c.add(m, d.Name, pub, def, d.Span)
	case *ast.Enum:
		def := &Def{Name: d.Name, Module: m.Name, Kind: DefEnum, Methods: map[string]*FnDef{}, pub: pub}
		def.Params = c.genericParams(d.Generics)
		c.add(m, d.Name, pub, def, d.Span)
	case *ast.Trait:
		// the traits of a native module are method tables for types that exist
		if !m.Native {
			c.add(m, d.Name, pub, &TraitDef{Name: d.Name, Module: m, Decl: d, Self: c.newParam("Self"), Methods: map[string]*FnDef{}}, d.Span)
		}
	case *ast.Const:
		c.add(m, d.Name, pub, &ConstDef{Name: d.Name, Module: m, Decl: d, pub: pub}, d.Span)
	case *ast.Alias:
		a := &AliasDef{Name: d.Name, Module: m, Decl: d}
		a.Params = c.genericParams(d.Generics)
		c.add(m, d.Name, pub, a, d.Span)
	case *ast.Group:
		c.add(m, d.Name, pub, &GroupDef{Name: d.Name, Module: m, Decl: d}, d.Span)
	}
}

func (c *checker) genericParams(gs []*ast.Generic) []*Param {
	out := make([]*Param, len(gs))
	for i, g := range gs {
		out[i] = c.newParam(g.Name)
	}

	return out
}

// imports brings in what the module uses.
func (c *checker) imports(m *Module) {
	for _, u := range m.File.Uses {
		mod, rest := c.usedModule(u.Path)
		if mod == nil {
			c.errorf(u, "unknown-module", "there is no module %s", strings.Join(u.Path, "::"))
			continue
		}

		var ent any = mod

		name := u.Path[len(u.Path)-1]

		switch len(rest) {
		case 0:
		case 1:
			d, ok := mod.Decls[rest[0]]
			if !ok {
				c.errorf(u, "unknown-name", "module %s has no %s", mod.Name, rest[0])
				continue
			}

			if !d.Pub {
				c.errorf(u, "private", "%s is not public in module %s", rest[0], mod.Name)
				continue
			}

			ent = d.Entity
		default:
			c.errorf(u, "unknown-name", "module %s has no %s", mod.Name, strings.Join(rest, "::"))
			continue
		}

		if u.Alias != "" {
			name = u.Alias
		}

		if _, dup := m.Decls[name]; dup {
			c.errorf(u, "duplicate", "%s is already declared in this module", name)
			continue
		}

		if _, dup := m.Imports[name]; dup {
			c.errorf(u, "duplicate", "%s is already used", name)
			continue
		}

		m.Imports[name] = ent
	}
}

// exportVariants lets Some, None, Ok and Err be written without Option:: and Result::.
func (c *checker) exportVariants() {
	for _, n := range []string{"Option", "Result"} {
		if dd := c.prelude.Decls[n]; dd == nil {
			continue
		} else if d, ok := dd.Entity.(*Def); ok {
			for _, v := range d.Variants {
				c.prelude.Decls[v.Name] = &Decl{Name: v.Name, Pub: true, Entity: &VariantRef{v}}
			}
		}
	}
}

func (c *checker) resolveDef(d *Def, m *Module) {
	if d.resolved {
		return
	}

	d.resolved = true

	var decl any

	for _, it := range m.File.Items {
		switch x := it.Decl.(type) {
		case *ast.Struct:
			if x.Name == d.Name {
				decl = x
			}
		case *ast.Enum:
			if x.Name == d.Name {
				decl = x
			}
		}
	}

	ts := newTScope(nil)
	if d.Params != nil {
		for _, p := range d.Params {
			ts.types[p.Name] = p
		}
	}

	switch x := decl.(type) {
	case *ast.Struct:
		d.Fields = c.fieldInfos(x.Fields, ts)
	case *ast.Enum:
		for _, v := range x.Variants {
			d.Variants = append(d.Variants, &Variant{Name: v.Name, Enum: d, Fields: c.fieldInfos(v.Fields, ts)})
		}
	}
}

func (c *checker) fieldInfos(fs []*ast.Field, ts *tscope) []*FieldInfo {
	out := make([]*FieldInfo, len(fs))
	for i, f := range fs {
		out[i] = &FieldInfo{f.Name, c.typeOf(f.Type, ts)}
	}

	return out
}

func (c *checker) resolveAlias(a *AliasDef) {
	if a.state == 2 {
		return
	}

	if a.state == 1 {
		c.errorf(a.Decl, "cycle", "the type %s is defined in terms of itself", a.Name)
		a.Type = bad

		return
	}

	a.state = 1
	saved := c.cur
	c.cur = a.Module

	ts := newTScope(nil)
	for _, p := range a.Params {
		ts.types[p.Name] = p
	}

	a.Type = c.typeOf(a.Decl.Type, ts)
	c.cur = saved
	a.state = 2
}

func (c *checker) resolveTrait(t *TraitDef) {
	ts := newTScope(nil)

	if len(t.Decl.Generics) > 0 {
		c.errorf(t.Decl, "unsupported", "a trait with parameters is not supported yet")
	}

	for _, md := range t.Decl.Methods {
		if _, dup := t.Methods[md.Name]; dup {
			c.errorf(md, "duplicate", "%s is declared twice in %s", md.Name, t.Name)
			continue
		}

		fd := &FnDef{Name: md.Name, Module: t.Module, Decl: md, Pub: true, Trait: t}
		c.fnSig(fd, ts, []*Param{t.Self}, t.Self)
		t.Methods[md.Name] = fd
		t.Order = append(t.Order, md.Name)
	}
}

func (c *checker) resolveFn(fd *FnDef) {
	c.fnSig(fd, nil, nil, nil)
}

// effectParamNames are the generic names of a function that stand for effects: the ones used after "with".
func effectParamNames(d *ast.Fn) map[string]bool {
	used := map[string]bool{}

	var walkType func(t ast.Type)

	paths := func(ps []*ast.Path) {
		for _, p := range ps {
			if len(p.Names) == 1 {
				used[p.Names[0]] = true
			}
		}
	}

	walkType = func(t ast.Type) {
		switch t := t.(type) {
		case *ast.TypeFn:
			paths(t.Effects)

			for _, p := range t.Params {
				walkType(p)
			}

			if t.Ret != nil {
				walkType(t.Ret)
			}
		case *ast.TypeName:
			for _, a := range t.Args {
				walkType(a)
			}
		}
	}

	paths(d.Effects)

	for _, p := range d.Params {
		if p.Type != nil {
			walkType(p.Type)
		}
	}

	if d.Ret != nil {
		walkType(d.Ret)
	}

	out := map[string]bool{}

	for _, g := range d.Generics {
		if used[g.Name] {
			out[g.Name] = true
		}
	}

	return out
}

// fnSig works out the type of a function from its signature. prefix are the type parameters that come from the impl or the
// trait it is in, and self is the type of self.
func (c *checker) fnSig(fd *FnDef, outer *tscope, prefix []*Param, self Type) {
	d := fd.Decl
	saved := c.cur
	c.cur = fd.Module

	defer func() { c.cur = saved }()

	ts := newTScope(outer)
	fd.TParams = append(fd.TParams, prefix...)
	effNames := effectParamNames(d)

	for _, g := range d.Generics {
		if effNames[g.Name] {
			ts.effs[g.Name] = "'" + g.Name
			fd.EParams = append(fd.EParams, "'"+g.Name)

			if len(g.Bounds) > 0 {
				c.errorf(g, "bound", "%s stands for effects, it can not have a bound", g.Name)
			}

			continue
		}

		p := c.newParam(g.Name)
		ts.types[g.Name] = p
		fd.TParams = append(fd.TParams, p)
	}

	for _, g := range d.Generics {
		if p, ok := ts.types[g.Name].(*Param); ok {
			p.Bounds = c.bounds(g.Bounds)
		}
	}

	fd.ts = ts

	var params []Type

	for i, p := range d.Params {
		switch {
		case p.Self:
			if self == nil {
				c.errorf(p, "self", "self is only for the methods of a type")
				params = append(params, bad)

				continue
			}

			if i != 0 {
				c.errorf(p, "self", "self has to be the first parameter")
			}

			fd.HasSelf = true

			params = append(params, self)
		default:
			params = append(params, c.typeOf(p.Type, ts))
		}
	}

	var ret Type = c.unit()
	if d.Ret != nil {
		ret = c.typeOf(d.Ret, ts)
	}

	fd.Type = &Fn{Params: params, Ret: ret, Eff: c.effects(d.Effects, ts)}
}

func (c *checker) bounds(ps []*ast.Path) []*TraitDef {
	var out []*TraitDef

	for _, p := range ps {
		ent, rest, _ := c.lookupPath(p.Names)

		if t, ok := ent.(*TraitDef); ok && len(rest) == 0 {
			out = append(out, t)
		} else {
			c.errorf(p, "unknown-name", "%s is not a trait", strings.Join(p.Names, "::"))
		}
	}

	return out
}

// effects turns the effects of a signature into a set: groups are replaced by what is in them.
func (c *checker) effects(ps []*ast.Path, ts *tscope) *Eff {
	var names []string

	for _, p := range ps {
		if len(p.Names) == 1 {
			if n, ok := ts.effNamed(p.Names[0]); ok {
				names = append(names, n)
				continue
			}

			if _, isType := ts.typeNamed(p.Names[0]); isType {
				c.errorf(p, "effect", "%s is a type parameter, and is used here for effects", p.Names[0])
				continue
			}
		}

		ent, rest, _ := c.lookupPath(p.Names)

		if g, ok := ent.(*GroupDef); ok && len(rest) == 0 {
			names = append(names, c.groupNames(g)...)
			continue
		}

		full := strings.Join(p.Names, "::")

		if c.known[full] || c.cur.Native {
			c.known[full] = true
			names = append(names, full)

			continue
		}

		c.errorf(p, "unknown-effect", "%s is not an effect the host knows, and it is not a group", full)
	}

	return newEff(names...)
}

// groupNames are the effects in a group, with the groups in it replaced too.
func (c *checker) groupNames(g *GroupDef) []string {
	switch g.state {
	case 2:
		return g.names
	case 1:
		saved := c.cur
		c.cur = g.Module
		c.errorf(g.Decl, "cycle", "the group %s has itself in it", g.Name)
		c.cur = saved

		return nil
	}

	g.state = 1
	saved := c.cur
	c.cur = g.Module

	e := c.effects(g.Decl.Effects, newTScope(nil))
	g.names = e.Names
	g.state = 2
	c.cur = saved

	return g.names
}

// typeOf is the type that is written.
func (c *checker) typeOf(t ast.Type, ts *tscope) Type {
	switch t := t.(type) {
	case nil:
		return bad
	case *ast.TypeFn:
		params := make([]Type, len(t.Params))
		for i, p := range t.Params {
			params[i] = c.typeOf(p, ts)
		}

		var ret Type = c.unit()
		if t.Ret != nil {
			ret = c.typeOf(t.Ret, ts)
		}

		return &Fn{params, ret, c.effects(t.Effects, ts)}
	case *ast.TypeName:
		if len(t.Path) == 1 {
			if p, ok := ts.typeNamed(t.Path[0]); ok {
				if len(t.Args) > 0 {
					c.errorf(t, "arity", "%s is a type parameter, it takes no types", t.Path[0])
				}

				return p
			}

			if _, isEff := ts.effNamed(t.Path[0]); isEff {
				c.errorf(t, "effect", "%s stands for effects, and is used here as a type", t.Path[0])
				return bad
			}
		}

		ent, rest, private := c.lookupPath(t.Path)

		if private != "" {
			c.errorf(t, "private", "%s is not public", private)
			return bad
		}

		if ent == nil || len(rest) > 0 {
			c.errorf(t, "unknown-name", "there is no type %s", strings.Join(t.Path, "::"))
			return bad
		}

		args := make([]Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = c.typeOf(a, ts)
		}

		switch e := ent.(type) {
		case *Def:
			if len(args) != len(e.Params) {
				c.errorf(t, "arity", "%s takes %d types, and %d are given", e.Name, len(e.Params), len(args))
				return bad
			}

			return &Con{e, args}
		case *AliasDef:
			c.resolveAlias(e)

			if len(args) != len(e.Params) {
				c.errorf(t, "arity", "%s takes %d types, and %d are given", e.Name, len(e.Params), len(args))
				return bad
			}

			s := &subst{types: map[*Param]Type{}}
			for i, p := range e.Params {
				s.types[p] = args[i]
			}

			return s.ty(e.Type)
		}

		c.errorf(t, "unknown-name", "%s is not a type", strings.Join(t.Path, "::"))
	}

	return bad
}

// nativeTrait adds the methods of a trait in a native module to the type it is named after, or the functions to the module.
func (c *checker) nativeTrait(m *Module, d *ast.Trait) {
	if d.Name == "Functions" {
		for _, md := range d.Methods {
			fd := &FnDef{Name: md.Name, Module: m, Decl: md, Pub: true}
			c.fnSig(fd, nil, nil, nil)
			c.add(m, md.Name, true, fd, md.Span)
		}

		return
	}

	def, ok := c.lookup(m, d.Name).(*Def)
	if !ok {
		c.errorf(d, "unknown-name", "%s is not a type: a trait of a native module is for the methods of a type", d.Name)
		return
	}

	if len(d.Generics) != len(def.Params) {
		c.errorf(d, "arity", "%s takes %d types", def.Name, len(def.Params))
		return
	}

	ts := newTScope(nil)
	args := make([]Type, len(def.Params))

	for i, g := range d.Generics {
		ts.types[g.Name] = def.Params[i]
		args[i] = def.Params[i]
	}

	self := &Con{def, args}

	for _, md := range d.Methods {
		fd := &FnDef{Name: md.Name, Module: m, Decl: md, Pub: true, Owner: def}
		c.fnSig(fd, ts, def.Params, self)

		if _, dup := def.Methods[md.Name]; dup {
			c.errorf(md, "duplicate", "%s has a method %s already", def.Name, md.Name)
			continue
		}

		def.Methods[md.Name] = fd
	}
}
