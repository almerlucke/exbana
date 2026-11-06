package check

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
)

// expr checks an expression and gives its type. want is the type that is expected there, if one is: it is where closures get
// the types of their parameters, and it is checked against what the expression is.
func (c *checker) expr(e ast.Expr, want Type) Type {
	t := c.exprOf(e, want)

	if want != nil && !c.unify(want, t) {
		c.mismatch(e, want, t)
		t = bad
	}

	c.types[e] = t

	return t
}

// hint lets an expected type decide what is not known yet, and does nothing if it does not fit.
func (c *checker) hint(want, got Type) {
	if want == nil {
		return
	}

	mark := c.mark()
	if !c.unify(want, got) {
		c.rollback(mark)
	}
}

func (c *checker) exprOf(e ast.Expr, want Type) Type {
	switch e := e.(type) {
	case *ast.Lit:
		switch e.Kind {
		case ast.LitInt:
			if _, err := strconv.ParseInt(strings.ReplaceAll(e.Text, "_", ""), 0, 64); err != nil {
				c.errorf(e, "number", "%s does not fit in an Int", e.Text)
			}

			return c.intT()
		case ast.LitFloat:
			if f, err := strconv.ParseFloat(strings.ReplaceAll(e.Text, "_", ""), 64); err != nil || math.IsInf(f, 0) {
				c.errorf(e, "number", "%s does not fit in a Float", e.Text)
			}

			return c.con(c.tFloat)
		case ast.LitText:
			return c.con(c.tText)
		}

		return c.boolT()
	case *ast.Name:
		return c.nameExpr(e)
	case *ast.List:
		var elem Type = c.newVar()

		if w, ok := prune(want).(*Con); ok && w.Def == c.list {
			elem = w.Args[0]
		}

		for _, x := range e.Elems {
			c.expr(x, elem)
		}

		return c.con(c.list, elem)
	case *ast.Map:
		var k, v Type = c.newVar(), c.newVar()

		if w, ok := prune(want).(*Con); ok && w.Def == c.mapDef {
			k, v = w.Args[0], w.Args[1]
		}

		for _, en := range e.Entries {
			c.expr(en.Key, k)
			c.expr(en.Value, v)
		}

		if len(e.Entries) > 0 && !c.isKey(k, map[*Def]bool{}) {
			c.errorf(e, "key", "%s can not be the key of a map: a key is an Int, a Text, a Bool, or a struct or enum made of those", prune(k))
		}

		return c.con(c.mapDef, k, v)
	case *ast.Call:
		return c.call(e, want)
	case *ast.Member:
		return c.member(e)
	case *ast.Update:
		return c.update(e)
	case *ast.Unary:
		return c.unary(e)
	case *ast.Binary:
		return c.binary(e)
	case *ast.Closure:
		return c.closure(e, want)
	case *ast.Cast:
		return c.cast(e)
	case *ast.Match:
		return c.matchExpr(e, want)
	case *ast.If:
		return c.ifExpr(e)
	}

	return bad
}

func (c *checker) nestedBlock(b *ast.Block) {
	for _, e := range append(append([]ast.Expr(nil), b.Requires...), b.Examples...) {
		c.errorf(e, "promise", "requires and example are for the start of a function")
	}

	c.push()

	for _, s := range b.Stmts {
		c.stmt(s)
	}

	c.pop()
}

func (c *checker) ifExpr(e *ast.If) Type {
	c.expr(e.Cond, c.boolT())
	c.nestedBlock(e.Then)

	switch el := e.Else.(type) {
	case *ast.Block:
		c.nestedBlock(el)
	case *ast.If:
		c.ifExpr(el)
	}

	return c.unit()
}

// useEffects notes that something with these effects is done: a function has to allow them, a closure has them.
func (c *checker) useEffects(at ast.Node, names []string) {
	if len(names) == 0 {
		return
	}

	f := c.fn

	if f.collect != nil {
		for _, n := range names {
			f.collect[n] = true
		}

		return
	}

	var missing []string

	for _, n := range names {
		if !f.allowed.has(n) {
			missing = append(missing, n)
		}
	}

	switch {
	case len(missing) == 0:
	case f.pure:
		c.errorf(at, "effect", "a promise only looks: it can not do %s", join(missing))
	default:
		all := join(append(append([]string(nil), f.allowed.names()...), missing...))
		c.errorf(at, "effect", "this needs the effect %s, and the signature of this function does not list it (it would be: with %s)", join(missing), all)
	}
}

func (c *checker) nameExpr(e *ast.Name) Type {
	if len(e.Path) == 1 {
		if l := c.scope.find(e.Path[0]); l != nil {
			return l.ty
		}

		if e.Path[0] == "Unit" {
			return c.unit()
		}
	}

	ent, rest, private := c.lookupPath(e.Path)

	if private != "" {
		c.errorf(e, "private", "%s is not public", private)
		return bad
	}

	if ent != nil {
		c.refs[e] = Ref{ent, rest}
	}

	switch ent := ent.(type) {
	case nil:
		c.unknownName(e)
		return bad
	case *FnDef:
		if len(rest) == 0 {
			fn, _ := c.instantiate(ent)
			if fn == nil {
				return bad
			}

			return fn
		}
	case *ConstDef:
		if len(rest) == 0 && ent.Type != nil {
			return ent.Type
		}
	case *VariantRef:
		if len(rest) == 0 {
			return c.unitVariant(e, ent.Variant)
		}
	case *Def:
		switch len(rest) {
		case 0:
			c.errorf(e, "type-as-value", "%s is a type: make a value of it with %s(...)", ent.Name, ent.Name)
			return bad
		case 1:
			if v := variantOf(ent, rest[0]); v != nil {
				return c.unitVariant(e, v)
			}

			if fd, ok := ent.Methods[rest[0]]; ok && fd.Type != nil {
				fn, _ := c.instantiate(fd)
				return fn
			}

			c.errorf(e, "unknown-name", "%s has no variant or function %s%s", ent.Name, rest[0], suggestion(rest[0], defNames(ent)))

			return bad
		}
	}

	c.errorf(e, "unknown-name", "%s is not a value", strings.Join(e.Path, "::"))

	return bad
}

func defNames(d *Def) []string {
	var out []string

	for _, v := range d.Variants {
		out = append(out, v.Name)
	}

	for n := range d.Methods {
		out = append(out, n)
	}

	return out
}

func variantOf(d *Def, name string) *Variant {
	for _, v := range d.Variants {
		if v.Name == name {
			return v
		}
	}

	return nil
}

func (c *checker) unitVariant(at ast.Node, v *Variant) Type {
	if len(v.Fields) > 0 {
		c.errorf(at, "arity", "%s carries %d values: write %s(...)", v.Name, len(v.Fields), v.Name)
		return bad
	}

	return c.enumType(v)
}

func (c *checker) enumType(v *Variant) *Con {
	args := make([]Type, len(v.Enum.Params))
	for i := range args {
		args[i] = c.newVar()
	}

	return &Con{v.Enum, args}
}

func (c *checker) unknownName(e *ast.Name) {
	var names []string

	for s := c.scope; s != nil; s = s.parent {
		for n := range s.vars {
			names = append(names, n)
		}
	}

	for n := range c.cur.Decls {
		names = append(names, n)
	}

	for n := range c.cur.Imports {
		names = append(names, n)
	}

	c.errorf(e, "unknown-name", "there is no %s%s", strings.Join(e.Path, "::"), suggestion(e.Path[len(e.Path)-1], names))
}

// suggestion is ", did you mean x?" for the name that is the closest to the one that is written, if one is close.
func suggestion(name string, names []string) string {
	best, bestDist := "", 3

	sort.Strings(names)

	for _, n := range names {
		if d := editDistance(strings.ToLower(name), strings.ToLower(n)); d < bestDist && n != name {
			best, bestDist = n, d
		}
	}

	if best == "" {
		return ""
	}

	return fmt.Sprintf(", did you mean %s?", best)
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i

		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}

			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}

		prev = cur
	}

	return prev[len(b)]
}

// call checks a call of a function, a method, a constructor, or a value that is a function.
func (c *checker) call(e *ast.Call, want Type) Type {
	switch f := e.Fun.(type) {
	case *ast.Member:
		return c.methodCall(e, f, want)
	case *ast.Name:
		if len(f.Path) == 1 && c.scope.find(f.Path[0]) != nil {
			return c.callValue(e)
		}

		ent, rest, private := c.lookupPath(f.Path)

		if private != "" {
			c.errorf(f, "private", "%s is not public", private)
			c.args(e, nil)

			return bad
		}

		if ent != nil {
			c.refs[f] = Ref{ent, rest}
		}

		switch ent := ent.(type) {
		case *FnDef:
			if len(rest) == 0 {
				return c.callFn(e, ent, nil, want)
			}
		case *VariantRef:
			if len(rest) == 0 {
				return c.construct(e, ent.Variant.Enum, ent.Variant.Name, ent.Variant.Fields, want)
			}
		case *Def:
			switch len(rest) {
			case 0:
				if ent.Kind == DefStruct {
					return c.construct(e, ent, ent.Name, ent.Fields, want)
				}

				c.errorf(f, "type-as-value", "%s is not made with %s(...): %s", ent.Name, ent.Name, c.howToMake(ent))
				c.args(e, nil)

				return bad
			case 1:
				if v := variantOf(ent, rest[0]); v != nil {
					return c.construct(e, ent, v.Name, v.Fields, want)
				}

				if fd, ok := ent.Methods[rest[0]]; ok {
					if fd.HasSelf {
						c.errorf(f, "self", "%s takes self: call it on a value, as x.%s(...)", rest[0], rest[0])
						c.args(e, nil)

						return bad
					}

					return c.callFn(e, fd, nil, want)
				}

				c.errorf(f, "unknown-name", "%s has no variant or function %s%s", ent.Name, rest[0], suggestion(rest[0], defNames(ent)))
				c.args(e, nil)

				return bad
			}
		}

		if ent == nil {
			c.unknownName(f)
			c.args(e, nil)

			return bad
		}
	}

	return c.callValue(e)
}

func (c *checker) howToMake(d *Def) string {
	if d.Kind == DefEnum {
		return "make one of its variants, as " + d.Name + "::" + firstVariant(d) + "(...)"
	}

	return "it has no constructor"
}

func firstVariant(d *Def) string {
	if len(d.Variants) > 0 {
		return d.Variants[0].Name
	}

	return "Variant"
}

// callValue calls something that has the type of a function.
func (c *checker) callValue(e *ast.Call) Type {
	ft := prune(c.expr(e.Fun, nil))

	fn, ok := ft.(*Fn)
	if !ok {
		if _, isBad := ft.(*Bad); !isBad {
			if _, isVar := ft.(*Var); isVar {
				c.errorf(e.Fun, "cannot-infer", "the type of this is not known yet, so it can not be called: say what it is")
			} else {
				c.errorf(e.Fun, "not-callable", "this is %s, and can not be called", ft)
			}
		}

		c.args(e, nil)

		return bad
	}

	c.args(e, fn.Params)
	c.useEffects(e, fn.Eff.names())

	return fn.Ret
}

// args checks the arguments of a call against the parameters.
func (c *checker) args(e *ast.Call, params []Type) {
	if params != nil && len(e.Args) != len(params) {
		c.errorf(e, "arity", "%d arguments are given, and %d are expected", len(e.Args), len(params))
		params = nil
	}

	labelled := false

	for i, a := range e.Args {
		if a.Label != "" && !labelled {
			labelled = true

			c.errorf(a, "label", "functions take their arguments by position: labels are for making a struct or a variant")
		}

		var want Type
		if params != nil && i < len(params) {
			want = params[i]
		}

		c.expr(a.Value, want)
	}
}

func (c *checker) callFn(e *ast.Call, fd *FnDef, recv Type, want Type) Type {
	if fd.Type == nil {
		c.args(e, nil)
		return bad
	}

	fn, s := c.instantiate(fd)
	params := fn.Params

	if recv != nil {
		if !c.unify(params[0], recv) {
			c.mismatch(e.Fun, params[0], recv)
		}

		params = params[1:]
	}

	c.hint(want, fn.Ret)
	c.args(e, params)

	c.useEffects(e, fn.Eff.names())
	c.checkBounds(e, fd, s)

	return fn.Ret
}

// checkBounds checks that the types a generic function was given have the traits it asks for.
func (c *checker) checkBounds(at ast.Node, fd *FnDef, s *subst) {
	for _, p := range fd.TParams {
		if len(p.Bounds) == 0 {
			continue
		}

		t := prune(s.types[p])

		if _, isVar := t.(*Var); isVar {
			c.errorf(at, "cannot-infer", "%s of %s has to be a type with %s, and which one it is can not be worked out", p.Name, fd.Name, boundNames(p.Bounds))
			continue
		}

		for _, b := range p.Bounds {
			if !c.implements(t, b) {
				c.errorf(at, "trait", "%s is not a type with the trait %s, which %s asks of %s", t, b.Name, fd.Name, p.Name)
			}
		}
	}
}

func boundNames(bs []*TraitDef) string {
	names := make([]string, len(bs))
	for i, b := range bs {
		names[i] = b.Name
	}

	return strings.Join(names, " + ")
}

func (c *checker) methodCall(e *ast.Call, f *ast.Member, want Type) Type {
	rt := prune(c.expr(f.X, nil))

	if _, isBad := rt.(*Bad); isBad {
		c.args(e, nil)
		return bad
	}

	fd, ok := c.findMethod(rt, f.Name)
	if !ok {
		if _, isVar := rt.(*Var); isVar {
			c.errorf(f, "cannot-infer", "the type of this is not known yet, so .%s can not be found: say what it is", f.Name)
		} else {
			c.errorf(f, "no-method", "%s has no method %s%s", rt, f.Name, suggestion(f.Name, c.methodNames(rt)))
		}

		c.args(e, nil)

		return bad
	}

	switch {
	case !fd.HasSelf:
		c.errorf(f, "self", "%s does not take self: call it as %s::%s(...)", f.Name, rt, f.Name)
		c.args(e, nil)

		return bad
	case fd.Impl != nil && fd.Impl.Trait == nil && !fd.Pub && fd.Module != c.cur:
		c.errorf(f, "private", "the method %s is not public", f.Name)
	}

	return c.callFn(e, fd, rt, want)
}

func (c *checker) methodNames(t Type) []string {
	var out []string

	switch t := t.(type) {
	case *Con:
		for n := range t.Def.Methods {
			out = append(out, n)
		}

		for _, impl := range t.Def.Impls {
			for n := range impl.Methods {
				out = append(out, n)
			}
		}
	case *Param:
		for _, b := range t.Bounds {
			for n := range b.Methods {
				out = append(out, n)
			}
		}
	case *Dyn:
		for n := range t.Trait.Methods {
			out = append(out, n)
		}
	}

	return out
}

// construct makes a value of a struct or a variant, from its fields by position or by label.
func (c *checker) construct(e *ast.Call, def *Def, name string, fields []*FieldInfo, want Type) Type {
	s := &subst{types: map[*Param]Type{}}
	args := make([]Type, len(def.Params))

	for i, p := range def.Params {
		args[i] = c.newVar()
		s.types[p] = args[i]
	}

	result := &Con{def, args}
	c.hint(want, result)

	types := make([]Type, len(fields))
	for i, f := range fields {
		types[i] = s.ty(f.Type)
	}

	labelled := 0

	for _, a := range e.Args {
		if a.Label != "" {
			labelled++
		}
	}

	switch {
	case labelled == 0:
		c.args(e, types)
	case labelled < len(e.Args):
		c.errorf(e, "label", "the arguments of %s are all given by label, or none are", name)
		c.args(e, nil)
	default:
		seen := map[string]bool{}

		for _, a := range e.Args {
			idx := -1

			for i, f := range fields {
				if f.Name == a.Label {
					idx = i
				}
			}

			var fieldNames []string
			for _, f := range fields {
				fieldNames = append(fieldNames, f.Name)
			}

			switch {
			case idx < 0:
				c.errorf(a, "label", "%s has no field %s%s", name, a.Label, suggestion(a.Label, fieldNames))
				c.expr(a.Value, nil)
			case seen[a.Label]:
				c.errorf(a, "label", "the field %s is given twice", a.Label)
				c.expr(a.Value, nil)
			default:
				seen[a.Label] = true

				c.expr(a.Value, types[idx])
			}
		}

		for _, f := range fields {
			if !seen[f.Name] {
				c.errorf(e, "label", "the field %s of %s is missing", f.Name, name)
			}
		}
	}

	return result
}

// cast makes a value into a dyn: one that is only known by a trait. It is where that is written, and it checks that the type of
// the value has the trait.
func (c *checker) cast(e *ast.Cast) Type {
	target := c.typeOf(e.Type, c.ts)
	xt := prune(c.expr(e.X, nil))

	d, ok := target.(*Dyn)
	if !ok {
		return bad
	}

	c.refs[e] = Ref{Entity: d.Trait}

	switch t := xt.(type) {
	case *Bad:
	case *Var:
		c.errorf(e.X, "cannot-infer", "the type of this is not known yet, so it can not be made into a %s: say what it is", target)
	case *Dyn:
		if t.Trait != d.Trait {
			c.errorf(e.X, "type-mismatch", "a %s is not made into a %s: it is only known by %s", t, d, t.Trait.Name)
		}
	default:
		if !c.implements(xt, d.Trait) {
			c.errorf(e.X, "trait", "%s does not have the trait %s, and can not be made into a %s", xt, d.Trait.Name, d)
		}
	}

	return target
}

func (c *checker) member(e *ast.Member) Type {
	xt := prune(c.expr(e.X, nil))

	con, ok := xt.(*Con)
	if !ok {
		if _, isBad := xt.(*Bad); !isBad {
			if _, isVar := xt.(*Var); isVar {
				c.errorf(e, "cannot-infer", "the type of this is not known yet, so .%s can not be found: say what it is", e.Name)
			} else {
				c.errorf(e, "no-field", "%s has no fields", xt)
			}
		}

		return bad
	}

	if con.Def.Kind == DefStruct {
		for _, f := range con.Def.Fields {
			if f.Name == e.Name {
				return c.fieldType(con, f)
			}
		}
	}

	if _, isMethod := c.findMethod(con, e.Name); isMethod {
		c.errorf(e, "method", "%s is a method of %s: call it with (...)", e.Name, con)
		return bad
	}

	var names []string
	for _, f := range con.Def.Fields {
		names = append(names, f.Name)
	}

	c.errorf(e, "no-field", "%s has no field %s%s", con, e.Name, suggestion(e.Name, names))

	return bad
}

func (c *checker) fieldType(con *Con, f *FieldInfo) Type {
	s := &subst{types: map[*Param]Type{}}
	for i, p := range con.Def.Params {
		s.types[p] = con.Args[i]
	}

	return s.ty(f.Type)
}

func (c *checker) update(e *ast.Update) Type {
	xt := prune(c.expr(e.X, nil))

	con, ok := xt.(*Con)
	if !ok || con.Def.Kind != DefStruct {
		if _, isBad := xt.(*Bad); !isBad {
			c.errorf(e.X, "type-mismatch", "with { ... } makes a changed copy of a struct, and this is %s", xt)
		}

		for _, f := range e.Fields {
			c.expr(f.Value, nil)
		}

		return xt
	}

	seen := map[string]bool{}

	for _, f := range e.Fields {
		var fi *FieldInfo

		for _, x := range con.Def.Fields {
			if x.Name == f.Name {
				fi = x
			}
		}

		switch {
		case fi == nil:
			var names []string
			for _, x := range con.Def.Fields {
				names = append(names, x.Name)
			}

			c.errorf(f, "no-field", "%s has no field %s%s", con, f.Name, suggestion(f.Name, names))
			c.expr(f.Value, nil)
		case seen[f.Name]:
			c.errorf(f, "label", "the field %s is given twice", f.Name)
			c.expr(f.Value, nil)
		default:
			seen[f.Name] = true

			c.expr(f.Value, c.fieldType(con, fi))
		}
	}

	return con
}

func (c *checker) isNumber(t Type) bool {
	con, ok := prune(t).(*Con)

	return ok && (con.Def == c.tInt || con.Def == c.tFloat)
}

func (c *checker) unary(e *ast.Unary) Type {
	switch e.Op {
	case "not":
		c.expr(e.X, c.boolT())
		return c.boolT()
	case "-":
		t := prune(c.expr(e.X, nil))
		if _, isBad := t.(*Bad); !isBad && !c.isNumber(t) {
			c.errorf(e, "type-mismatch", "- is for Int and Float, and this is %s", t)
			return bad
		}

		return t
	}

	return c.try(e)
}

// try unwraps a Result or an Option, and ends the function with the error or the None.
func (c *checker) try(e *ast.Unary) Type {
	t := prune(c.expr(e.X, nil))

	con, ok := t.(*Con)
	if !ok || con.Def.Module != "prelude" || (con.Def.Name != "Result" && con.Def.Name != "Option") {
		if _, isBad := t.(*Bad); !isBad {
			c.errorf(e, "try", "try is for a Result or an Option, and this is %s", t)
		}

		return bad
	}

	// what the function gives has to be able to carry the error: a Result with the same error, or an Option
	ret := prune(c.fn.ret)

	if c.fn.pure {
		c.errorf(e, "try", "a promise only gives true or false: try can not be used in it")
		return con.Args[0]
	}

	var carrier Type

	if con.Def.Name == "Result" {
		carrier = &Con{con.Def, []Type{c.newVar(), con.Args[1]}}
	} else {
		carrier = &Con{con.Def, []Type{c.newVar()}}
	}

	if !c.unify(ret, carrier) {
		switch rc, isCon := ret.(*Con); {
		case isCon && rc.Def == con.Def && con.Def.Name == "Result":
			c.errorf(e, "try", "this try has the error %s, and the function gives a Result with the error %s: change one of them explicitly, as .map_error(...)", con.Args[1], rc.Args[1])
		default:
			c.errorf(e, "try", "try can end this function with the %s, and the function gives %s: it has to give a %s", con.Def.Name, ret, con.Def.Name)
		}
	}

	return con.Args[0]
}

func (c *checker) hasFn(t Type) bool {
	switch t := prune(t).(type) {
	case *Fn:
		return true
	case *Con:
		for _, a := range t.Args {
			if c.hasFn(a) {
				return true
			}
		}

		for _, f := range t.Def.Fields {
			if c.hasFn(f.Type) {
				return true
			}
		}

		for _, v := range t.Def.Variants {
			for _, f := range v.Fields {
				if c.hasFn(f.Type) {
					return true
				}
			}
		}
	}

	return false
}

func (c *checker) binary(e *ast.Binary) Type {
	if e.Op == "and" || e.Op == "or" {
		c.expr(e.L, c.boolT())
		c.expr(e.R, c.boolT())

		return c.boolT()
	}

	lt := c.expr(e.L, nil)
	c.expr(e.R, lt)

	t := prune(lt)

	switch t.(type) {
	case *Bad:
		return bad
	case *Var:
		c.errorf(e, "cannot-infer", "the type of the left side of %s is not known yet: say what it is", e.Op)
		return bad
	}

	con, isCon := t.(*Con)

	switch e.Op {
	case "==", "!=":
		if c.hasFn(t) {
			c.errorf(e, "type-mismatch", "functions can not be compared")
		}

		return c.boolT()
	case "<", "<=", ">", ">=":
		if !isCon || (con.Def != c.tInt && con.Def != c.tFloat && con.Def != c.tText) {
			c.errorf(e, "type-mismatch", "%s is for Int, Float and Text, and this is %s", e.Op, t)
		}

		return c.boolT()
	case "+":
		if !isCon || (con.Def != c.tInt && con.Def != c.tFloat && con.Def != c.tText) {
			c.errorf(e, "type-mismatch", "+ is for Int, Float and Text, and this is %s", t)
			return bad
		}

		return t
	case "%":
		if !isCon || con.Def != c.tInt {
			c.errorf(e, "type-mismatch", "%% is for Int, and this is %s", t)
			return bad
		}

		return t
	}

	if !c.isNumber(t) {
		c.errorf(e, "type-mismatch", "%s is for Int and Float, and this is %s", e.Op, t)
		return bad
	}

	return t
}

func (c *checker) closure(e *ast.Closure, want Type) Type {
	hint, _ := prune(want).(*Fn)

	if hint != nil && len(hint.Params) != len(e.Params) {
		c.errorf(e, "arity", "this closure takes %d parameters, and %d are expected here", len(e.Params), len(hint.Params))

		hint = nil
	}

	params := make([]Type, len(e.Params))

	for i, p := range e.Params {
		switch {
		case p.Type != nil:
			params[i] = c.typeOf(p.Type, c.ts)

			if hint != nil {
				c.hint(hint.Params[i], params[i])
			}
		case hint != nil:
			params[i] = hint.Params[i]
		default:
			c.errorf(p, "cannot-infer", "the type of %s can not be worked out here: write it as %s: Type", p.Name, p.Name)

			params[i] = bad
		}
	}

	var ret Type

	switch {
	case e.Ret != nil:
		ret = c.typeOf(e.Ret, c.ts)

		if hint != nil {
			c.hint(hint.Ret, ret)
		}
	case hint != nil:
		ret = hint.Ret
	default:
		ret = c.newVar()
	}

	saved, savedScope := c.fn, c.scope
	c.fn = &fnCtx{ret: ret, collect: map[string]bool{}, depth: saved.depth + 1, parent: saved, pure: saved.pure}
	c.scope = &scope{parent: savedScope, vars: map[string]*local{}}

	for i, p := range e.Params {
		c.declareLocal(p, p.Name, params[i], false)
	}

	if e.Body != nil {
		c.blockBody(e.Body)

		if rt := prune(ret); !c.isUnit(rt) {
			if _, isVar := rt.(*Var); !isVar && !terminates(e.Body) {
				c.errorSpan(ast.Span{Begin: e.Body.End - 1, End: e.Body.End}, "return-missing", "this closure gives a %s, and not every way through it ends in a return", rt)
			}
		}

		// no return: the closure gives nothing
		if _, isVar := prune(ret).(*Var); isVar {
			c.unify(ret, c.unit())
		}
	} else {
		c.expr(e.Value, ret)
	}

	names := make([]string, 0, len(c.fn.collect))
	for n := range c.fn.collect {
		names = append(names, n)
	}

	c.inferred(c.fn)

	c.fn, c.scope = saved, savedScope

	return &Fn{Params: params, Ret: ret, Eff: newEff(names...)}
}

func (c *checker) matchExpr(e *ast.Match, want Type) Type {
	st := c.expr(e.X, nil)

	res := want
	if res == nil {
		res = c.newVar()
	}

	var rows [][]*npat

	for _, arm := range e.Arms {
		c.push()

		np := c.pattern(arm.Pattern, st, func(name string, ty Type, at ast.Node) {
			c.declareLocal(at, name, ty, false)
		})
		rows = append(rows, []*npat{np})

		c.expr(arm.Value, res)
		c.pop()
	}

	if w, ok := c.missing(rows, []Type{st}); !ok {
		c.errorf(e, "not-exhaustive", "this match does not cover every case: %s is left out", w)
	}

	return res
}

// isKey says whether a type can be the key of a map.
func (c *checker) isKey(t Type, seen map[*Def]bool) bool {
	switch t := prune(t).(type) {
	case *Var, *Bad:
		return true
	case *Con:
		switch t.Def.Kind {
		case DefPrim:
			return t.Def == c.tInt || t.Def == c.tText || t.Def == c.tBool
		case DefBuiltin:
			return false
		}

		if seen[t.Def] {
			return true
		}

		seen[t.Def] = true

		for _, a := range t.Args {
			if !c.isKey(a, seen) {
				return false
			}
		}

		for _, f := range t.Def.Fields {
			if !c.isKey(c.fieldType(t, f), seen) {
				return false
			}
		}

		for _, v := range t.Def.Variants {
			for _, f := range v.Fields {
				if !c.isKey(c.fieldType(t, f), seen) {
					return false
				}
			}
		}

		return true
	}

	return false
}
