package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
	"github.com/almerlucke/exbana/v3/examples/covenant/check"
)

// HostFunc is a function the host provides, for a signature of a native module. It is given the arguments, with self first for a
// method, and gives the result. A function that can fail gives a Result: an error it returns is a fault of the program.
type HostFunc func(ctx context.Context, args []Value) (Value, error)

// Config says what the program may do, and how much.
type Config struct {
	// Host has the functions of the native modules that the checker was given: "fs::read_all" for the function read_all of the
	// module fs, and "Path.parent" for the method parent of the type Path.
	Host map[string]HostFunc

	// Grant is the effects the program may have. A function can only be called if the effects in its signature are granted.
	Grant []string

	// MaxSteps ends a call that takes more steps than this, with a *Limit. The default is 100 million, and -1 is no limit.
	MaxSteps int64

	// MaxDepth is how deep calls can nest before it is a fault. The default is 2000.
	MaxDepth int

	// Bytecode runs the program as bytecode, on a machine with a stack, and not by walking the tree. What it does is the same,
	// faults and all, and it is faster. A step is an instruction then, and not an expression.
	Bytecode bool
}

// Program is a checked program, ready to run. It can be used from several goroutines: a call has its own state.
type Program struct {
	res    *check.Result
	cfg    Config
	grant  map[string]bool
	defs   map[string]*check.Def // of the prelude
	consts map[*check.ConstDef]Value

	// the bytecode, made when it is first needed
	mu          sync.Mutex
	protos      map[*check.FnDef]*proto
	constProtos map[*check.ConstDef]*proto
	queue       []func() error
}

// New makes a program of a result of the checker. A result with problems in it can not be run.
func New(res *check.Result, cfg Config) (*Program, error) {
	if !res.OK() {
		return nil, fmt.Errorf("the program has problems, and can not be run:\n%v", res.Error())
	}

	if cfg.MaxSteps == 0 {
		cfg.MaxSteps = 100_000_000
	}

	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = 2000
	}

	p := &Program{
		res: res, cfg: cfg, grant: map[string]bool{}, defs: map[string]*check.Def{}, consts: map[*check.ConstDef]Value{},
		protos: map[*check.FnDef]*proto{}, constProtos: map[*check.ConstDef]*proto{},
	}

	for _, g := range cfg.Grant {
		p.grant[g] = true
	}

	pre := res.Modules["prelude"]
	for name, d := range pre.Decls {
		if def, ok := d.Entity.(*check.Def); ok {
			p.defs[name] = def
		}
	}

	// every function the host has to provide is provided
	var missing []string

	for _, m := range res.Modules {
		if !m.Native || m.Name == "prelude" {
			continue
		}

		for name, d := range m.Decls {
			if fd, ok := d.Entity.(*check.FnDef); ok && fd.Decl.Body == nil {
				if _, ok := cfg.Host[m.Name+"::"+name]; !ok {
					missing = append(missing, m.Name+"::"+name)
				}
			}
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("the host does not provide %s", strings.Join(sorted(missing), ", "))
	}

	return p, nil
}

// Fault is the program ending because of a bug in it: an overflow, a division by zero, a requires that is false, a call stack
// that is too deep. A program can not catch it.
type Fault struct {
	Kind      string // "overflow", "division", "requires", "stack", "range", "host"
	Msg       string
	Module    string
	File      string // the file in the module, as it was given to the checker
	Line, Col int
	Stack     []string // the functions that were running, the innermost first
}

func (f *Fault) Error() string {
	where := f.File
	if where == "" {
		where = f.Module
	}

	s := fmt.Sprintf("fault (%s) at %s:%d:%d: %s", f.Kind, where, f.Line, f.Col, f.Msg)
	if len(f.Stack) > 0 {
		s += "\n  in " + strings.Join(f.Stack, "\n  in ")
	}

	return s
}

// Limit is a call that was ended because it used more than it was given, or was cancelled. Nothing is wrong with the program.
type Limit struct {
	Kind string // "steps" or "cancelled"
	Err  error
}

func (l *Limit) Error() string {
	if l.Err != nil {
		return "limit (" + l.Kind + "): " + l.Err.Error()
	}

	return "limit (" + l.Kind + ")"
}

func (l *Limit) Unwrap() error { return l.Err }

// Denied is a call of a function that needs effects the host did not grant.
type Denied struct {
	Fn      string
	Effects []string
}

func (d *Denied) Error() string {
	return fmt.Sprintf("%s needs the effects %s, which were not granted", d.Fn, strings.Join(d.Effects, ", "))
}

// Call runs a function of a module with the arguments, and gives what it returns. The function has to be one the program
// declares, and its effects have to be granted.
func (p *Program) Call(ctx context.Context, module, function string, args ...Value) (Value, error) {
	m := p.res.Modules[module]
	if m == nil {
		return nil, fmt.Errorf("there is no module %s", module)
	}

	d, ok := m.Decls[function]
	if !ok {
		return nil, fmt.Errorf("module %s has no %s", module, function)
	}

	fd, ok := d.Entity.(*check.FnDef)
	if !ok {
		return nil, fmt.Errorf("%s::%s is not a function", module, function)
	}

	if len(args) != len(fd.Type.Params) {
		return nil, fmt.Errorf("%s::%s takes %d arguments, and %d are given", module, function, len(fd.Type.Params), len(args))
	}

	var denied []string

	for _, e := range fd.Type.Eff.All() {
		if !p.grant[e] {
			denied = append(denied, strings.TrimPrefix(e, "'"))
		}
	}

	if len(denied) > 0 {
		return nil, &Denied{module + "::" + function, denied}
	}

	if p.cfg.Bytecode {
		mc := newVM(p, ctx, m)

		v, err := mc.callDef(fd, args, nil)
		if err != nil {
			return nil, publicError(err)
		}

		return v, nil
	}

	mc := &machine{p: p, ctx: ctx, cur: m}

	v, err := mc.callDef(fd, args, nil)
	if err != nil {
		return nil, mc.public(err)
	}

	return v, nil
}

// ExampleResult is the outcome of an example of a function.
type ExampleResult struct {
	Module, File, Function string
	Line                   int
	Text                   string // the example as it is written
	Err                    error  // nil if it is true
}

// Examples runs all the examples of the program, and says which of them are true. An example is a fact about a function, and
// has no effects, so it needs nothing from the host.
func (p *Program) Examples(ctx context.Context) []ExampleResult {
	var out []ExampleResult

	for _, name := range sorted(keys(p.res.Modules)) {
		m := p.res.Modules[name]
		if m.Native {
			continue
		}

		run := func(fn string, b *ast.Block) {
			if b == nil {
				return
			}

			for _, ex := range b.Examples {
				file, line, _ := m.Locate(ex.At().Begin)
				r := ExampleResult{Module: m.Name, File: file, Function: fn, Line: line, Text: m.Source[ex.At().Begin:ex.At().End]}

				v, err := p.example(ctx, m, ex)

				switch {
				case err != nil:
					r.Err = publicError(err)
				case v != true:
					r.Err = errors.New("it is false")
				}

				out = append(out, r)
			}
		}

		for _, it := range m.File.Items {
			switch d := it.Decl.(type) {
			case *ast.Fn:
				run(d.Name, d.Body)
			case *ast.Impl:
				for _, f := range d.Methods {
					run(f.Name, f.Body)
				}
			}
		}
	}

	return out
}

// example runs the expression of an example.
func (p *Program) example(ctx context.Context, m *check.Module, ex ast.Expr) (Value, error) {
	if !p.cfg.Bytecode {
		mc := &machine{p: p, ctx: ctx, cur: m}
		return mc.eval(ex, newEnv(nil))
	}

	p.mu.Lock()
	pr, err := p.exprProto(m, "example", ex)

	if err == nil {
		err = p.drain()
	}

	p.mu.Unlock()

	if err != nil {
		return nil, err
	}

	return newVM(p, ctx, m).invoke(pr, nil, nil, nil)
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)

	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}

	return out
}

// Some, None, Ok and Err make the values of Option and Result, for a host function to give.

// Some is an Option with a value.
func (p *Program) Some(v Value) Value { return p.variant("Option", "Some", v) }

// None is an Option without one.
func (p *Program) None() Value { return p.variant("Option", "None") }

// Ok is a Result that worked.
func (p *Program) Ok(v Value) Value { return p.variant("Result", "Ok", v) }

// Err is a Result that did not.
func (p *Program) Err(v Value) Value { return p.variant("Result", "Err", v) }

func (p *Program) variant(enum, name string, fields ...Value) Value {
	d := p.defs[enum]

	for _, v := range d.Variants {
		if v.Name == name {
			return &Enum{d, v, fields}
		}
	}

	panic("the prelude has no " + enum + "::" + name)
}

// Variant makes a value of an enum of a module, with the fields of the variant by position.
func (p *Program) Variant(module, enum, variant string, fields ...Value) (Value, error) {
	m := p.res.Modules[module]
	if m == nil {
		return nil, fmt.Errorf("there is no module %s", module)
	}

	d, ok := m.Decls[enum]
	if !ok {
		return nil, fmt.Errorf("module %s has no %s", module, enum)
	}

	def, ok := d.Entity.(*check.Def)
	if !ok || def.Kind != check.DefEnum {
		return nil, fmt.Errorf("%s::%s is not an enum", module, enum)
	}

	v := variantOf(def, variant)
	if v == nil || len(v.Fields) != len(fields) {
		return nil, fmt.Errorf("%s::%s has no variant %s with %d fields", module, enum, variant, len(fields))
	}

	return &Enum{def, v, fields}, nil
}

// Struct makes a value of a struct of a module, with its fields by position. Host is for what the host keeps in it.
func (p *Program) Struct(module, name string, host any, fields ...Value) (*Struct, error) {
	m := p.res.Modules[module]
	if m == nil {
		return nil, fmt.Errorf("there is no module %s", module)
	}

	d, ok := m.Decls[name]
	if !ok {
		return nil, fmt.Errorf("module %s has no %s", module, name)
	}

	def, ok := d.Entity.(*check.Def)
	if !ok || def.Kind != check.DefStruct || len(def.Fields) != len(fields) {
		return nil, fmt.Errorf("%s::%s is not a struct with %d fields", module, name, len(fields))
	}

	return &Struct{def, fields, host}, nil
}
