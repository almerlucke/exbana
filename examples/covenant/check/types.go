// Package check is the type and effect checker of Covenant. It reads the files of a program (see the ast package), works out the
// type of every expression, and says where the program is wrong: a type that does not fit, a name that is not there, a case a
// match leaves out, a function that does more than its signature says.
//
// What it checks, in short:
//
//   - Types are checked with local inference: signatures are always written out, and what is inside a function is worked out
//     from them, the way a closure gets its parameter types from where it is passed.
//   - Effects are checked like types: a call needs its effects to be in the signature of the function that makes it, and a
//     closure has the effects of what it calls. A function can be generic in its effects, as in
//     fn map<U, E>(self, f: Fn(T) with E -> U) with E -> List<U>, which is what lets map take a closure that reads a file.
//   - A match has to cover every case, a let can not be assigned twice, a closure can not change what it captures, a function
//     that gives a result has to give it on every path, and the Result of a call can not be dropped.
//
// What it does not check yet is in the README of the language.
package check

import (
	"sort"
	"strings"
)

// Type is a type in the checker: *Con, *Fn, *Dyn, *Var, *Param or *Bad.
type Type interface{ String() string }

// DefKind says what a Def is.
type DefKind int

// The kinds of definitions of types.
const (
	DefPrim    DefKind = iota // Int, Float, Text, Bool, Unit
	DefBuiltin                // List, Map, Set: types the host provides
	DefStruct
	DefEnum
)

// Def is the definition of a type with a name: a primitive, a type that is built in, a struct or an enum.
type Def struct {
	Name     string
	Module   string
	Kind     DefKind
	Params   []*Param
	Fields   []*FieldInfo // of a struct
	Variants []*Variant   // of an enum
	Methods  map[string]*FnDef
	Impls    []*Impl

	pub      bool
	resolved bool
}

// FieldInfo is a field of a struct or of a variant, with its type in terms of the parameters of the definition.
type FieldInfo struct {
	Name string
	Type Type
}

// Variant is a case of an enum.
type Variant struct {
	Name   string
	Enum   *Def
	Fields []*FieldInfo
}

// Con is a type with a name, with the types it is given: Int, List<Int>, Result<Port, ParseError>.
type Con struct {
	Def  *Def
	Args []Type
}

// Fn is the type of a function: Fn(Int, Text) with fs::read -> Bool.
type Fn struct {
	Params []Type
	Ret    Type
	Eff    *Eff
}

// Var is a type that is not known yet. It is bound to a type when the checker finds out which.
type Var struct {
	id   int
	link Type
}

// Param is a type that stands for any type, inside a generic function or a definition: the T in List<T>.
type Param struct {
	Name   string
	id     int
	Bounds []*TraitDef
}

// Dyn is a value of any type that has a trait: dyn Area. What it is made from is not known, and only the methods of the trait
// can be called on it.
type Dyn struct{ Trait *TraitDef }

func (t *Dyn) String() string { return "dyn " + t.Trait.Name }

// Bad is the type of what has already been reported as wrong, so that one mistake is one message. It goes with every type.
type Bad struct{}

var bad = &Bad{}

func (*Bad) String() string { return "?" }

func (t *Param) String() string { return t.Name }

func (t *Var) String() string {
	if t.link != nil {
		return t.link.String()
	}

	return "_"
}

func (t *Con) String() string {
	if len(t.Args) == 0 {
		return t.Def.Name
	}

	args := make([]string, len(t.Args))
	for i, a := range t.Args {
		args[i] = a.String()
	}

	return t.Def.Name + "<" + strings.Join(args, ", ") + ">"
}

func (t *Fn) String() string {
	params := make([]string, len(t.Params))
	for i, p := range t.Params {
		params[i] = p.String()
	}

	s := "Fn(" + strings.Join(params, ", ") + ")"

	if e := t.Eff.String(); e != "" {
		s += " with " + e
	}

	if c, ok := prune(t.Ret).(*Con); !ok || c.Def.Name != "Unit" {
		s += " -> " + t.Ret.String()
	}

	return s
}

// prune follows the variables that are bound to the type they stand for.
func prune(t Type) Type {
	for {
		v, ok := t.(*Var)
		if !ok || v.link == nil {
			return t
		}

		t = v.link
	}
}

// Eff is a set of effects: names of effects, and variables for the effects that a generic function takes from what it is
// passed. A name that starts with a quote ('E) is an effect parameter of the function the checker is in.
type Eff struct {
	Names []string
	Vars  []*EffVar
}

// EffVar is the effects a call takes from its arguments: it collects the effects of the closures that are passed for it.
type EffVar struct {
	id    int
	names map[string]bool
}

var noEffects = &Eff{}

// names is all the effects in the set, with what the variables have collected, in order.
func (e *Eff) names() []string {
	if e == nil {
		return nil
	}

	seen := map[string]bool{}

	for _, n := range e.Names {
		seen[n] = true
	}

	for _, v := range e.Vars {
		for n := range v.names {
			seen[n] = true
		}
	}

	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}

	sort.Strings(out)

	return out
}

// All are the effects in the set, in order.
func (e *Eff) All() []string { return e.names() }

func (e *Eff) has(name string) bool {
	return slicesContains(e.names(), name)
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}

	return false
}

func (e *Eff) String() string {
	names := e.names()
	for i, n := range names {
		names[i] = strings.TrimPrefix(n, "'")
	}

	return strings.Join(names, " + ")
}

func newEff(names ...string) *Eff {
	if len(names) == 0 {
		return noEffects
	}

	e := &Eff{}
	for _, n := range names {
		if !slicesContains(e.Names, n) {
			e.Names = append(e.Names, n)
		}
	}

	sort.Strings(e.Names)

	return e
}

// subst replaces parameters by types, and effect parameters by variables that collect effects.
type subst struct {
	types map[*Param]Type
	effs  map[string]*EffVar
}

func (s *subst) ty(t Type) Type {
	switch t := prune(t).(type) {
	case *Param:
		if r, ok := s.types[t]; ok {
			return r
		}

		return t
	case *Con:
		if len(t.Args) == 0 {
			return t
		}

		args := make([]Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = s.ty(a)
		}

		return &Con{t.Def, args}
	case *Fn:
		params := make([]Type, len(t.Params))
		for i, p := range t.Params {
			params[i] = s.ty(p)
		}

		return &Fn{params, s.ty(t.Ret), s.eff(t.Eff)}
	}

	return t
}

func (s *subst) eff(e *Eff) *Eff {
	if len(s.effs) == 0 || e == nil {
		return e
	}

	out := &Eff{Vars: append([]*EffVar(nil), e.Vars...)}

	for _, n := range e.Names {
		if v, ok := s.effs[n]; ok {
			out.Vars = append(out.Vars, v)
		} else {
			out.Names = append(out.Names, n)
		}
	}

	return out
}

// occurs says whether the variable is in the type, which would make a type that is inside itself.
func occurs(v *Var, t Type) bool {
	switch t := prune(t).(type) {
	case *Var:
		return t == v
	case *Con:
		for _, a := range t.Args {
			if occurs(v, a) {
				return true
			}
		}
	case *Fn:
		for _, p := range t.Params {
			if occurs(v, p) {
				return true
			}
		}

		return occurs(v, t.Ret)
	}

	return false
}
