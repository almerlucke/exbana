package check

import (
	"fmt"
	"sort"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant"
	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
	"github.com/almerlucke/exbana/v3/examples/covenant/token"
)

// Config is what the host tells the checker.
type Config struct {
	// Effects are the effects the host knows, like "fs::read". An effect in a signature has to be one of these, a group, or
	// one a native module declares in the signatures of its functions.
	Effects []string

	// Native are modules that the host provides, by name, as Covenant source with signatures and no bodies. A trait named
	// Functions holds the functions of the module; a trait named like a type adds methods to that type. A native module
	// can declare types as structs without fields.
	Native map[string]string
}

// Diagnostic is something wrong in the program. It is data: the code says what kind of thing it is, so that a tool can act on it.
type Diagnostic struct {
	Module    string
	Span      ast.Span
	Line, Col int
	Code      string
	Msg       string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s:%d:%d: %s", d.Module, d.Line, d.Col, d.Msg)
}

// Result is what the checker found.
type Result struct {
	Diagnostics []Diagnostic

	// Types are the types of the expressions of the program, as far as they are known.
	Types map[ast.Node]Type

	Modules map[string]*Module
}

// OK says whether the program has no problems.
func (r *Result) OK() bool { return len(r.Diagnostics) == 0 }

// Error is all the diagnostics as one error, or nil.
func (r *Result) Error() error {
	if r.OK() {
		return nil
	}

	lines := make([]string, len(r.Diagnostics))
	for i, d := range r.Diagnostics {
		lines[i] = d.String()
	}

	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}

// TypeOf is the type of an expression, as text, or "" if the checker did not see it.
func (r *Result) TypeOf(e ast.Node) string {
	if t, ok := r.Types[e]; ok {
		return prune(t).String()
	}

	return ""
}

type checker struct {
	cfg     Config
	diags   []Diagnostic
	mods    map[string]*Module
	prelude *Module
	cur     *Module
	nextID  int
	trail   []func()
	known   map[string]bool
	types   map[ast.Node]Type
	sources map[*Module]*covenant.File
	implOf  map[*ast.Impl]*Impl
	ts      *tscope // the type parameters of the function that is being checked

	tInt, tFloat, tText, tBool, tUnit *Def
	list, mapDef, set                 *Def

	scope *scope
	fn    *fnCtx
}

// Check checks a program: its modules, by name, as source. The prelude (Option, Result, the methods of the types that are built
// in) is part of every program.
func Check(cfg Config, sources map[string]string) *Result {
	c := &checker{
		cfg: cfg, mods: map[string]*Module{}, known: map[string]bool{}, types: map[ast.Node]Type{}, sources: map[*Module]*covenant.File{}, implOf: map[*ast.Impl]*Impl{},
	}

	for _, e := range cfg.Effects {
		c.known[e] = true
	}

	c.run(sources)

	sort.SliceStable(c.diags, func(i, j int) bool {
		a, b := c.diags[i], c.diags[j]
		if a.Module != b.Module {
			return a.Module < b.Module
		}

		return a.Span.Begin < b.Span.Begin
	})

	return &Result{Diagnostics: c.diags, Types: c.types, Modules: c.mods}
}

// errorf reports a problem in the module that is being checked.
func (c *checker) errorf(at ast.Node, code, format string, args ...any) {
	var sp ast.Span
	if at != nil {
		sp = at.At()
	}

	c.errorSpan(sp, code, format, args...)
}

func (c *checker) errorSpan(sp ast.Span, code, format string, args ...any) {
	d := Diagnostic{Span: sp, Code: code, Msg: fmt.Sprintf(format, args...)}

	if c.cur != nil {
		d.Module = c.cur.Name
		d.Line, d.Col = token.Position(c.cur.Source, sp.Begin)
	}

	c.diags = append(c.diags, d)
}

func (c *checker) newVar() *Var {
	c.nextID++

	return &Var{id: c.nextID}
}

func (c *checker) newParam(name string) *Param {
	c.nextID++

	return &Param{Name: name, id: c.nextID}
}

func (c *checker) newEffVar() *EffVar {
	c.nextID++

	return &EffVar{id: c.nextID, names: map[string]bool{}}
}

func (c *checker) con(d *Def, args ...Type) *Con { return &Con{d, args} }

func (c *checker) unit() Type  { return c.con(c.tUnit) }
func (c *checker) boolT() Type { return c.con(c.tBool) }
func (c *checker) intT() Type  { return c.con(c.tInt) }
func (c *checker) isUnit(t Type) bool {
	con, ok := prune(t).(*Con)

	return ok && con.Def == c.tUnit
}

func join(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = strings.TrimPrefix(n, "'")
	}

	return strings.Join(out, " + ")
}
