package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
	"github.com/almerlucke/exbana/v3/examples/covenant/run"
)

// Manifest is what a program offers as tools.
type Manifest struct {
	Tools   []Tool    `json:"tools"`
	Skipped []Skipped `json:"skipped,omitempty"`
}

// Tool is a public function of a program, as an agent needs to know it.
type Tool struct {
	Name         string         `json:"name"` // module.function
	Module       string         `json:"module"`
	Function     string         `json:"function"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema"`
	Effects      []string       `json:"effects"`
	Examples     []string       `json:"examples,omitempty"`
}

// Skipped is a public function that is not a tool, and why.
type Skipped struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Describe makes the manifest of a program that has been checked: every public function of every module that the program has
// (and not the host's). They are in the order of the names.
func Describe(res *check.Result) (*Manifest, error) {
	if !res.OK() {
		return nil, fmt.Errorf("the program has problems:\n%v", res.Error())
	}

	m := &Manifest{Tools: []Tool{}}

	var modules []string
	for name, mod := range res.Modules {
		if !mod.Native {
			modules = append(modules, name)
		}
	}

	sort.Strings(modules)

	for _, name := range modules {
		mod := res.Modules[name]

		var fns []string
		for fn, d := range mod.Decls {
			if _, ok := d.Entity.(*check.FnDef); ok && d.Pub {
				fns = append(fns, fn)
			}
		}

		sort.Strings(fns)

		for _, fn := range fns {
			fd := mod.Decls[fn].Entity.(*check.FnDef)
			full := name + "." + fn

			tool, why := describe(mod, fd)
			if why != "" {
				m.Skipped = append(m.Skipped, Skipped{full, why})
				continue
			}

			m.Tools = append(m.Tools, tool)
		}
	}

	return m, nil
}

func describe(mod *check.Module, fd *check.FnDef) (Tool, string) {
	if len(fd.TParams) > 0 || len(fd.EParams) > 0 {
		return Tool{}, "it is generic"
	}

	seen := map[*check.Def]bool{}

	for i, p := range fd.Type.Params {
		if why := supported(p, seen); why != "" {
			return Tool{}, fmt.Sprintf("parameter %s: %s", fd.Decl.Params[i].Name, why)
		}
	}

	if why := supported(fd.Type.Ret, seen); why != "" {
		return Tool{}, "the result: " + why
	}

	in, out := newSchemer(), newSchemer()

	props := map[string]any{}
	required := []any{}

	for i, p := range fd.Type.Params {
		props[fd.Decl.Params[i].Name] = in.of(p)
		required = append(required, fd.Decl.Params[i].Name)
	}

	t := Tool{
		Name: mod.Name + "." + fd.Name, Module: mod.Name, Function: fd.Name, Description: docComment(mod.Source, fd.Decl.Begin),
		InputSchema: in.withDefs(map[string]any{
			"type": "object", "properties": props, "required": required, "additionalProperties": false,
		}),
		OutputSchema: out.withDefs(out.of(fd.Type.Ret)),
		Effects:      []string{},
	}

	for _, e := range fd.Type.Eff.All() {
		t.Effects = append(t.Effects, e)
	}

	for _, ex := range fd.Decl.Body.Examples {
		sp := ex.At()
		t.Examples = append(t.Examples, mod.Source[sp.Begin:sp.End])
	}

	return t, ""
}

// docComment is the comment on the lines just above a declaration, without the slashes.
func docComment(src string, begin int) string {
	lineStart := strings.LastIndex(src[:begin], "\n") + 1
	lines := strings.Split(strings.TrimSuffix(src[:lineStart], "\n"), "\n")

	var doc []string

	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(l, "//") {
			break
		}

		doc = append([]string{strings.TrimSpace(strings.TrimPrefix(l, "//"))}, doc...)
	}

	return strings.Join(doc, "\n")
}

// Call runs a public function of a program with arguments in JSON, and gives the result in JSON. The arguments are an object by the
// names of the parameters, or an array in their order.
func Call(ctx context.Context, prog *run.Program, res *check.Result, module, function string, args []byte) ([]byte, error) {
	mod := res.Modules[module]
	if mod == nil {
		return nil, fmt.Errorf("there is no module %s", module)
	}

	d, ok := mod.Decls[function]

	var fd *check.FnDef
	if ok {
		fd, ok = d.Entity.(*check.FnDef)
	}

	if !ok || mod.Native {
		return nil, fmt.Errorf("module %s has no function %s", module, function)
	}

	if !d.Pub {
		return nil, fmt.Errorf("%s::%s is not public: only a public function is a tool", module, function)
	}

	if _, why := describe(mod, fd); why != "" {
		return nil, fmt.Errorf("%s::%s is not a tool: %s", module, function, why)
	}

	c := NewCodec(prog)

	vals, err := c.decodeArgs(fd, args)
	if err != nil {
		return nil, err
	}

	v, err := prog.Call(ctx, module, function, vals...)
	if err != nil {
		return nil, err
	}

	return c.Encode(fd.Type.Ret, v)
}

func (c *Codec) decodeArgs(fd *check.FnDef, raw []byte) ([]run.Value, error) {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		raw = []byte("{}")
	}

	n := len(fd.Type.Params)
	vals := make([]run.Value, n)

	if raw[0] == '[' {
		items, err := array(raw)
		if err != nil {
			return nil, err
		}

		if len(items) != n {
			return nil, fmt.Errorf("%s takes %d arguments, and %d are given", fd.Name, n, len(items))
		}

		for i, x := range items {
			if vals[i], err = c.Decode(fd.Type.Params[i], x, fd.Decl.Params[i].Name); err != nil {
				return nil, err
			}
		}

		return vals, nil
	}

	entries, err := object(raw)
	if err != nil {
		return nil, errors.New(`the arguments are an object by the names of the parameters, {"name": value}, or an array`)
	}

	seen := make([]bool, n)

	for _, e := range entries {
		idx := -1

		for i, p := range fd.Decl.Params {
			if p.Name == e.key {
				idx = i
			}
		}

		switch {
		case idx < 0:
			return nil, fmt.Errorf("%s has no parameter %s", fd.Name, e.key)
		case seen[idx]:
			return nil, fmt.Errorf("the argument %s is there twice", e.key)
		}

		seen[idx] = true

		if vals[idx], err = c.Decode(fd.Type.Params[idx], e.raw, e.key); err != nil {
			return nil, err
		}
	}

	for i, p := range fd.Decl.Params {
		if !seen[i] {
			return nil, fmt.Errorf("the argument %s is missing", p.Name)
		}
	}

	return vals, nil
}

// JSON is the manifest as JSON text, with indentation.
func (m *Manifest) JSON() ([]byte, error) { return json.MarshalIndent(m, "", "  ") }
