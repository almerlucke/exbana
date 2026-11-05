package check

import (
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
)

// npat is a pattern as the check for missing cases sees it: anything, or a constructor with patterns for what it holds, or a
// literal.
type npat struct {
	wild bool
	ctor string // the name of the variant, "true", "false", the struct, or "()" for Unit
	lit  string // for a number or a text
	args []*npat
}

var wildPat = &npat{wild: true}

// pattern checks a pattern against the type it is matched with, tells bind about the names it binds, and gives the pattern in
// the form the check for missing cases wants.
func (c *checker) pattern(p ast.Pattern, t Type, bind func(name string, ty Type, at ast.Node)) *npat {
	switch p := p.(type) {
	case *ast.PatWild:
		return wildPat
	case *ast.PatLit:
		return c.literalPattern(p, t)
	case *ast.PatName:
		return c.namePattern(p, t, bind)
	}

	return wildPat
}

func (c *checker) literalPattern(p *ast.PatLit, t Type) *npat {
	var lt Type

	switch p.Lit.Kind {
	case ast.LitInt:
		lt = c.intT()
	case ast.LitFloat:
		c.errorf(p, "pattern", "a Float is not matched with a pattern: compare it with ==")
		return wildPat
	case ast.LitText:
		lt = c.con(c.tText)
	default:
		lt = c.boolT()
	}

	if !c.unify(t, lt) {
		c.errorf(p, "type-mismatch", "this pattern is %s, and the value is %s", lt, prune(t))
		return wildPat
	}

	if p.Lit.Kind == ast.LitBool {
		return &npat{ctor: p.Lit.Text}
	}

	return &npat{lit: p.Lit.Text}
}

// ctorOf finds what a pattern name stands for, given the type that is matched: a variant of the enum, or the struct itself.
func (c *checker) ctorOf(path []string, t Type) (def *Def, v *Variant, ok bool) {
	con, isCon := prune(t).(*Con)

	if len(path) == 1 && isCon {
		if vv := variantOf(con.Def, path[0]); vv != nil {
			return con.Def, vv, true
		}

		if con.Def.Kind == DefStruct && con.Def.Name == path[0] {
			return con.Def, nil, true
		}
	}

	ent, rest, _ := c.lookupPath(path)

	switch ent := ent.(type) {
	case *VariantRef:
		if len(rest) == 0 {
			return ent.Variant.Enum, ent.Variant, true
		}
	case *Def:
		if len(rest) == 0 && ent.Kind == DefStruct {
			return ent, nil, true
		}

		if len(rest) == 1 {
			if vv := variantOf(ent, rest[0]); vv != nil {
				return ent, vv, true
			}
		}
	}

	return nil, nil, false
}

func (c *checker) namePattern(p *ast.PatName, t Type, bind func(string, Type, ast.Node)) *npat {
	// a lower case name on its own is a new variable
	if len(p.Path) == 1 && !p.HasArgs && lowerName(p.Path[0]) {
		bind(p.Path[0], t, p)
		return wildPat
	}

	def, v, ok := c.ctorOf(p.Path, t)
	if !ok {
		name := strings.Join(p.Path, "::")

		switch {
		case len(p.Path) == 1 && !lowerName(p.Path[0]):
			c.errorf(p, "unknown-name", "%s is not a variant here (a name that starts with a capital letter is never a new variable)%s", name, suggestion(name, c.variantNames(t)))
		default:
			c.errorf(p, "unknown-name", "there is no variant or struct %s", name)
		}

		c.bindAll(p.Args, bind)

		return wildPat
	}

	if v != nil {
		c.refs[p] = Ref{Entity: v}
	} else {
		c.refs[p] = Ref{Entity: def}
	}

	con, isCon := prune(t).(*Con)
	if !isCon || con.Def != def {
		if _, isBad := prune(t).(*Bad); !isBad {
			c.errorf(p, "type-mismatch", "this pattern is for %s, and the value is %s", def.Name, prune(t))
		}

		c.bindAll(p.Args, bind)

		return wildPat
	}

	var fields []*FieldInfo

	name := def.Name

	if v != nil {
		fields, name = v.Fields, v.Name
	} else {
		fields = def.Fields
	}

	switch {
	case len(fields) > 0 && !p.HasArgs:
		c.errorf(p, "arity", "%s holds %d values: write %s(...)", name, len(fields), name)
		return wildPat
	case len(fields) == 0 && p.HasArgs:
		c.errorf(p, "arity", "%s holds nothing: write %s", name, name)
		return wildPat
	case len(p.Args) != len(fields):
		c.errorf(p, "arity", "%s holds %d values, and the pattern has %d", name, len(fields), len(p.Args))
		c.bindAll(p.Args, bind)

		return wildPat
	}

	np := &npat{ctor: name}

	for i, a := range p.Args {
		np.args = append(np.args, c.pattern(a, c.fieldType(con, fields[i]), bind))
	}

	return np
}

func (c *checker) bindAll(ps []ast.Pattern, bind func(string, Type, ast.Node)) {
	for _, a := range ps {
		c.pattern(a, bad, bind)
	}
}

func (c *checker) variantNames(t Type) []string {
	var out []string

	if con, ok := prune(t).(*Con); ok {
		for _, v := range con.Def.Variants {
			out = append(out, v.Name)
		}
	}

	return out
}

// ctorInfo is a way a value of a type can be made, for the check for missing cases.
type ctorInfo struct {
	name   string
	fields []Type
}

// ctorsOf are all the ways to make a value of the type, if there are only a few: a Bool, a Unit, a struct, an enum. Anything
// else (an Int, a Text, a List) has too many to name.
func (c *checker) ctorsOf(t Type) ([]ctorInfo, bool) {
	con, ok := prune(t).(*Con)
	if !ok {
		return nil, false
	}

	switch con.Def {
	case c.tBool:
		return []ctorInfo{{"true", nil}, {"false", nil}}, true
	case c.tUnit:
		return []ctorInfo{{"()", nil}}, true
	}

	switch con.Def.Kind {
	case DefStruct:
		var fs []Type
		for _, f := range con.Def.Fields {
			fs = append(fs, c.fieldType(con, f))
		}

		return []ctorInfo{{con.Def.Name, fs}}, true
	case DefEnum:
		out := make([]ctorInfo, len(con.Def.Variants))

		for i, v := range con.Def.Variants {
			var fs []Type
			for _, f := range v.Fields {
				fs = append(fs, c.fieldType(con, f))
			}

			out[i] = ctorInfo{v.Name, fs}
		}

		return out, true
	}

	return nil, false
}

// missing says whether the rows of patterns cover every value of the types. If they do not, it gives an example of a value that
// no row matches.
//
// A constructor is only taken apart if a pattern names it. If the patterns name all the constructors of the type, each of them
// is looked at with what is in it; if not, the value that is left out is one that none of the rows with a name in that place
// can match, so only the rows with "anything" there matter. That is also what makes it end for a type that has itself in it.
func (c *checker) missing(rows [][]*npat, tys []Type) (witness string, covered bool) {
	if len(tys) == 0 {
		if len(rows) > 0 {
			return "", true
		}

		return "", false
	}

	ctors, finite := c.ctorsOf(tys[0])

	named := map[string]bool{}

	for _, r := range rows {
		if !r[0].wild && r[0].ctor != "" {
			named[r[0].ctor] = true
		}
	}

	if finite && len(named) == len(ctors) {
		for _, ct := range ctors {
			var spec [][]*npat

			for _, r := range rows {
				switch {
				case r[0].wild:
					row := make([]*npat, 0, len(ct.fields)+len(r)-1)
					for range ct.fields {
						row = append(row, wildPat)
					}

					spec = append(spec, append(row, r[1:]...))
				case r[0].ctor == ct.name:
					spec = append(spec, append(append([]*npat(nil), r[0].args...), r[1:]...))
				}
			}

			inner := append(append([]Type(nil), ct.fields...), tys[1:]...)

			if w, ok := c.missing(spec, inner); !ok {
				return ctorWitness(ct, w), false
			}
		}

		return "", true
	}

	var rest [][]*npat

	for _, r := range rows {
		if r[0].wild {
			rest = append(rest, r[1:])
		}
	}

	w, ok := c.missing(rest, tys[1:])
	if ok {
		return "", true
	}

	if finite {
		for _, ct := range ctors {
			if !named[ct.name] {
				return ctorWitness(ct, ""), false
			}
		}
	}

	return "_" + witnessRest(w), false
}

// ctorWitness is a value that starts with the constructor, written the way it would be in a pattern.
func ctorWitness(ct ctorInfo, rest string) string {
	if len(ct.fields) == 0 {
		return ct.name
	}

	parts := make([]string, len(ct.fields))
	for i := range parts {
		parts[i] = "_"
	}

	// the first field that is not covered is the one that is named
	if first, _, ok := strings.Cut(rest, " "); ok || rest != "" {
		parts[0] = first
	}

	return ct.name + "(" + strings.Join(parts, ", ") + ")"
}

func witnessRest(w string) string {
	if w == "" {
		return ""
	}

	return " " + w
}
