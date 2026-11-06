package check

import (
	"fmt"
	"sort"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
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
// Line and Col are in the file, and Span is in the text of the module (see Module.Source).
type Diagnostic struct {
	Module    string
	File      string
	Span      ast.Span
	Line, Col int
	Code      string
	Msg       string
}

func (d Diagnostic) String() string {
	where := d.File
	if where == "" {
		where = d.Module
	}

	return fmt.Sprintf("%s:%d:%d: %s", where, d.Line, d.Col, d.Msg)
}

// Source is a file of a module.
type Source struct {
	Name string // as it is called in messages: the path
	Text string
}

// Result is what the checker found.
type Result struct {
	Diagnostics []Diagnostic

	// Types are the types of the expressions of the program, as far as they are known.
	Types map[ast.Node]Type

	// Refs say what a name stands for, where it is not a variable of the function: a function, a constant, a type, a variant.
	// The keys are the *ast.Name of an expression, and the *ast.PatName of a pattern (then the Entity is a *Variant or a *Def).
	Refs map[ast.Node]Ref

	Modules map[string]*Module
}

// Ref is what a name stands for. Rest are the names after the ones that were followed to the Entity: the variant or the
// function of a type, as in Shape::Circle.
type Ref struct {
	Entity any // *FnDef, *ConstDef, *Def, *VariantRef, *Variant, ...
	Rest   []string
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
	refs    map[ast.Node]Ref
	implOf  map[*ast.Impl]*Impl
	ts      *tscope // the type parameters of the function that is being checked

	tInt, tFloat, tText, tBool, tUnit *Def
	list, mapDef, set                 *Def

	scope *scope
	fn    *fnCtx
}

// Check checks a program of modules that are a file each: their names, and their source. The prelude (Option, Result, the methods
// of the types that are built in) is part of every program.
func Check(cfg Config, sources map[string]string) *Result {
	modules := make(map[string][]Source, len(sources))

	for name, text := range sources {
		modules[name] = []Source{{Name: name + ".cov", Text: text}}
	}

	return CheckModules(cfg, modules)
}

// CheckModules checks a program of modules that are one or more files each: the files of a module share their names, whatever
// is private in one of them can be used in the others, and what a module uses (with use) is for all of its files.
func CheckModules(cfg Config, sources map[string][]Source) *Result {
	c := &checker{
		cfg: cfg, mods: map[string]*Module{}, known: map[string]bool{}, types: map[ast.Node]Type{}, refs: map[ast.Node]Ref{}, implOf: map[*ast.Impl]*Impl{},
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

	return &Result{Diagnostics: c.diags, Types: c.types, Refs: c.refs, Modules: c.mods}
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
		d.File, d.Line, d.Col = c.cur.Locate(sp.Begin)
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
