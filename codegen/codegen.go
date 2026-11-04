// Package codegen writes a parser as Go source from a grammar made of patterns. The grammar is the specification and the
// test: design and check it with the patterns, then generate code that does the same without any of the machinery the
// patterns need to be flexible: no closures, no tree of match objects, no backtracking. The generated code does not use
// this library at all.
//
//	src, err := codegen.Generate(grammar, codegen.Config[rune, runes.Pos]{Package: "calc", Name: "Calc"})
//	os.WriteFile("calc_parser.go", src, 0o644)
//
// # What can be generated
//
// A parser that never backtracks. Every repetition has to be possessive and every alternation orthogonal, either because the
// grammar says so (SetPossessive, SetOrthogonal) or because the optimize package found it to be safe (run
// optimize.Optimize first). The generated parser then matches exactly what the grammar matches. A grammar that still has
// places that backtrack is refused, and the error lists them; Config.Commit generates code for it anyway, with "first match wins"
// where the grammar would have tried more, which is the meaning the grammar gets when optimize.Result.Force is applied.
//
// Left recursive rules are refused, write them as a loop. Leaves have to be declarative (entity.Between, In, Equals,
// vector.Text and the like) so that the generator can see what they accept; a leaf made from a function needs a
// Config.Class that says what it is as Go code.
//
// # Any element type
//
// The generated parser reads a slice of any element type, with an int as position. Runes, bytes, integers, strings and
// floats work out of the box. For another type, like a struct for a token, set Config.ElemType and, for the leaves that
// are functions, Config.Class.
//
// # What the parser returns
//
// A flat tree of the rules that matched (their names are constants), in a slice of small nodes, and a SyntaxError with
// the farthest failure and what was expected there, formatted like ebnf.FarthestLog. Evaluating the tree is up to you.
package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
)

// Config says how to generate the parser.
type Config[T, P any] struct {
	// Package is the name of the package of the generated file.
	Package string

	// Name is put in front of the exported names, so that several parsers can live in one package: Name "Json" gives
	// JsonParser, JsonTree and JsonSyntaxError. It can be empty.
	Name string

	// ElemType is the Go type of the elements the parser reads. The default is rune for int32, byte for uint8, and the
	// name of the type otherwise.
	ElemType string

	// Format writes an element as a Go expression. The default handles the numeric types, strings and booleans.
	Format func(T) string

	// Class gives a leaf that is decided by a function, which the generator can not look into, as a Go boolean expression
	// over the element x: for example `x.Kind == KindPlus`. Return false for a leaf it does not know.
	Class func(*ebnf.Node[T, P]) (expr string, ok bool)

	// Imports are packages the generated file has to import for what Format and Class produce.
	Imports []string

	// Commit generates code for repetitions and alternations that backtrack as if they did not: possessive and orthogonal.
	Commit bool

	// MaxDepth limits how deeply rules can nest; deeper than this the parser stops with an error instead of overflowing
	// the stack. The default is 100000.
	MaxDepth int
}

// Error is returned by Generate when the grammar can not be generated, with everything that is in the way.
type Error struct {
	Problems []string
}

func (e *Error) Error() string {
	return fmt.Sprintf("codegen: %d problems:\n  %s", len(e.Problems), strings.Join(e.Problems, "\n  "))
}

// Generate returns the source of a parser for the grammar.
func Generate[T, P any](p ebnf.Pattern[T, P], cfg Config[T, P]) ([]byte, error) {
	if cfg.Package == "" {
		return nil, fmt.Errorf("codegen: Config.Package is required")
	}

	g, err := ebnf.Analyze(p)
	if err != nil {
		return nil, err
	}

	c := newCompiler(g, cfg)

	c.validate()

	if len(c.problems) > 0 {
		return nil, &Error{Problems: c.problems}
	}

	return c.generate()
}

type compiler[T, P any] struct {
	g   *ebnf.Grammar[T, P]
	cfg Config[T, P]

	elem   string
	isText bool // runes and bytes: errors have a line and a column, and nodes have text

	problems []string

	// names
	used      map[string]bool
	ruleFn    map[string]string
	ruleConst map[string]string
	counter   int

	funcs strings.Builder
	lits  strings.Builder
}

func newCompiler[T, P any](g *ebnf.Grammar[T, P], cfg Config[T, P]) *compiler[T, P] {
	c := &compiler[T, P]{
		g: g, cfg: cfg,
		used:   map[string]bool{},
		ruleFn: map[string]string{}, ruleConst: map[string]string{},
	}

	c.elem = cfg.ElemType
	if c.elem == "" {
		switch t := reflect.TypeFor[T](); t.Kind() {
		case reflect.Int32:
			c.elem = "rune"
		case reflect.Uint8:
			c.elem = "byte"
		default:
			c.elem = t.String()
		}
	}

	c.isText = c.elem == "rune" || c.elem == "byte"

	if c.cfg.MaxDepth <= 0 {
		c.cfg.MaxDepth = 100000
	}

	return c
}

func (c *compiler[T, P]) problem(format string, args ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, args...))
}

// validate finds everything that can not be generated.
func (c *compiler[T, P]) validate() {
	var visit func(n *ebnf.Node[T, P], rule string)

	visit = func(n *ebnf.Node[T, P], rule string) {
		where := ""
		if rule != "" {
			where = " in " + rule
		}

		switch n.Kind {
		case ebnf.KindOpaque:
			c.problem("a pattern that can not be looked into%s: %s (write it with the declarative patterns, or implement ebnf.Structured)", where, n.String())
		case ebnf.KindLiteral:
			if !n.Exact {
				c.problem("a literal that is compared with a function%s: %s (use vector.Literal or vector.Text)", where, n.String())
			} else {
				for _, v := range n.Values {
					if _, err := c.value(v); err != nil {
						c.problem("%v", err)
						break
					}
				}
			}
		case ebnf.KindClass:
			if !n.Exact {
				if c.cfg.Class == nil {
					c.problem("a class decided by a function%s: %s (use entity.Between, In, Equals and the like, or set Config.Class)", where, classLabel(n))
				} else if _, ok := c.cfg.Class(n); !ok {
					c.problem("Config.Class does not know the class decided by a function%s: %s", where, classLabel(n))
				}
			} else {
				for _, r := range n.Ranges {
					for _, v := range r {
						if _, err := c.value(v); err != nil {
							c.problem("%v", err)
							return
						}
					}
				}
			}
		case ebnf.KindRepeat:
			if !n.Possessive && !c.cfg.Commit {
				c.problem("a repetition that backtracks%s: %s (run optimize.Optimize, mark it possessive, or set Config.Commit)", where, n.String())
			}
		case ebnf.KindAlt:
			if !n.Orthogonal && !c.cfg.Commit {
				c.problem("an alternation that backtracks%s: %s (run optimize.Optimize, mark it orthogonal, or set Config.Commit)", where, n.String())
			}
		}

		for _, ch := range n.Children {
			visit(ch, rule)
		}
	}

	visit(c.g.Root, "")

	for _, name := range c.g.Order {
		def := c.g.Rules[name]
		if def.LeftRecursive {
			c.problem("rule %s is left recursive (write it as a loop: a left recursive rule offers all of its matches)", name)
		}

		visit(def.Children[0], name)
	}
}

func classLabel[T, P any](n *ebnf.Node[T, P]) string {
	switch {
	case n.ID != "":
		return n.ID
	case n.Text != "":
		return n.Text
	}

	return "a class"
}

// value writes an element as a Go expression.
func (c *compiler[T, P]) value(v T) (string, error) {
	if c.cfg.Format != nil {
		return c.cfg.Format(v), nil
	}

	rv := reflect.ValueOf(v)

	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if c.elem == "rune" {
			return strconv.QuoteRune(rune(rv.Int())), nil
		}

		return strconv.FormatInt(rv.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10), nil
	case reflect.String:
		return strconv.Quote(rv.String()), nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(rv.Float(), 'g', -1, 64), nil
	case reflect.Bool:
		return strconv.FormatBool(rv.Bool()), nil
	}

	return "", fmt.Errorf("elements of type %s can not be written as Go code: set Config.Format", reflect.TypeFor[T]())
}

// camel makes a Go identifier from a name: "string literal" is StringLiteral
func camel(s string) string {
	var sb strings.Builder

	up := true

	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			up = true
			continue
		}

		if up {
			r = unicode.ToUpper(r)
			up = false
		}

		sb.WriteRune(r)
	}

	out := sb.String()
	if out == "" {
		return "Rule"
	}

	if unicode.IsDigit([]rune(out)[0]) {
		return "X" + out
	}

	return out
}

// unique returns the name, or the name with a number if it was taken.
func (c *compiler[T, P]) unique(name string) string {
	candidate := name

	for i := 2; c.used[candidate]; i++ {
		candidate = name + strconv.Itoa(i)
	}

	c.used[candidate] = true

	return candidate
}

func (c *compiler[T, P]) next(prefix string) string {
	c.counter++

	return prefix + strconv.Itoa(c.counter)
}

// description is what a failed leaf is called in a syntax error: its name or its print text, like ebnf.Describe.
func description[T, P any](n *ebnf.Node[T, P]) string {
	if n.ID != "" {
		return n.ID
	}

	return n.Text
}

func quote(s string) string { return strconv.Quote(s) }

// classCond writes the test of a class as a Go boolean expression over x.
func (c *compiler[T, P]) classCond(n *ebnf.Node[T, P]) string {
	if !n.Exact {
		expr, _ := c.cfg.Class(n)
		return expr
	}

	if len(n.Ranges) == 0 {
		if n.Negated {
			return "true"
		}

		return "false"
	}

	parts := make([]string, len(n.Ranges))

	for i, r := range n.Ranges {
		lo, _ := c.value(r[0])
		hi, _ := c.value(r[1])

		if reflect.DeepEqual(r[0], r[1]) {
			parts[i] = "x == " + lo
		} else {
			parts[i] = "x >= " + lo + " && x <= " + hi
		}
	}

	cond := strings.Join(parts, " || ")
	if len(parts) > 1 {
		cond = "(" + cond + ")"
	}

	if n.Negated {
		return "!" + ternary(len(parts) > 1, cond, "("+cond+")")
	}

	return cond
}

func ternary[V any](cond bool, yes, no V) V {
	if cond {
		return yes
	}

	return no
}

// emit writes the method that matches the node and returns its name. It takes a position and returns the position after
// the match, or -1.
func (c *compiler[T, P]) emit(n *ebnf.Node[T, P]) string {
	if n.Kind == ebnf.KindRef {
		return c.ruleFn[n.ID]
	}

	name := c.next("m")

	// the method is written in a buffer of its own and added when it is complete: the methods of the parts are added
	// while it is written, and must not end up in the middle of it
	var b strings.Builder

	f := &b

	fmt.Fprintf(f, "func (p *\x00P\x00) %s(pos int) int {\n", name)

	switch n.Kind {
	case ebnf.KindLiteral:
		c.emitLiteral(f, n, name)
	case ebnf.KindClass:
		fmt.Fprintf(f, "\tif pos < len(p.in) {\n\t\tx := p.in[pos]\n\t\tif %s {\n\t\t\treturn pos + 1\n\t\t}\n\t}\n", c.classCond(n))
		fmt.Fprintf(f, "\tp.fail(pos, %s)\n\treturn -1\n", quote(description(n)))
	case ebnf.KindEnd:
		fmt.Fprintf(f, "\tif pos == len(p.in) {\n\t\treturn pos\n\t}\n\tp.fail(pos, %s)\n\treturn -1\n", quote(description(n)))
	case ebnf.KindSeq:
		parts := make([]string, len(n.Children))
		for i, ch := range n.Children {
			parts[i] = c.emit(ch)
		}

		fmt.Fprintf(f, "\tq := pos\n")

		for _, part := range parts {
			fmt.Fprintf(f, "\tif q = p.%s(q); q < 0 {\n\t\treturn -1\n\t}\n", part)
		}

		fmt.Fprintf(f, "\treturn q\n")
	case ebnf.KindAlt:
		parts := make([]string, len(n.Children))
		for i, ch := range n.Children {
			parts[i] = c.emit(ch)
		}

		fmt.Fprintf(f, "\tmark := len(p.nodes)\n")

		for _, part := range parts {
			fmt.Fprintf(f, "\tif q := p.%s(pos); q >= 0 {\n\t\treturn q\n\t}\n\tp.nodes = p.nodes[:mark]\n", part)
		}

		if n.ID != "" {
			fmt.Fprintf(f, "\tp.fail(pos, %s)\n", quote(n.ID))
		}

		fmt.Fprintf(f, "\treturn -1\n")
	case ebnf.KindRepeat:
		item := c.emit(n.Children[0])

		fmt.Fprintf(f, "\tq, n := pos, 0\n")

		if n.Max > 0 {
			fmt.Fprintf(f, "\tfor n < %d {\n", n.Max)
		} else {
			fmt.Fprintf(f, "\tfor {\n")
		}

		fmt.Fprintf(f, "\t\tmark := len(p.nodes)\n\t\tr := p.%s(q)\n\t\tif r < 0 {\n\t\t\tp.nodes = p.nodes[:mark]\n\t\t\tbreak\n\t\t}\n", item)
		fmt.Fprintf(f, "\t\tn++\n\t\tif r == q {\n\t\t\tbreak\n\t\t}\n\t\tq = r\n\t}\n")

		if n.Min > 0 {
			fmt.Fprintf(f, "\tif n < %d {\n", n.Min)

			if n.ID != "" {
				fmt.Fprintf(f, "\t\tp.fail(pos, %s)\n", quote(n.ID))
			}

			fmt.Fprintf(f, "\t\treturn -1\n\t}\n")
		}

		fmt.Fprintf(f, "\treturn q\n")
	case ebnf.KindExcept:
		must, exc := c.emit(n.Children[0]), c.emit(n.Children[1])

		fmt.Fprintf(f, "\tmark := len(p.nodes)\n\tp.silent++\n\te := p.%s(pos)\n\tp.silent--\n\tp.nodes = p.nodes[:mark]\n", exc)
		fmt.Fprintf(f, "\tif e >= 0 {\n\t\treturn -1\n\t}\n\treturn p.%s(pos)\n", must)
	}

	fmt.Fprintf(f, "}\n\n")

	c.funcs.WriteString(b.String())

	return name
}

func (c *compiler[T, P]) emitLiteral(f *strings.Builder, n *ebnf.Node[T, P], name string) {
	vals := make([]string, len(n.Values))

	for i, v := range n.Values {
		vals[i], _ = c.value(v)
	}

	switch {
	case len(vals) == 0:
		fmt.Fprintf(f, "\treturn pos\n")
		return
	case len(vals) <= 8:
		conds := make([]string, len(vals))
		for i, v := range vals {
			conds[i] = fmt.Sprintf("p.in[pos+%d] == %s", i, v)
		}

		fmt.Fprintf(f, "\tif pos+%d <= len(p.in) && %s {\n\t\treturn pos + %d\n\t}\n", len(vals), strings.Join(conds, " && "), len(vals))
	default:
		lit := "\x00X\x00" + name + "Literal"
		fmt.Fprintf(&c.lits, "var %s = [...]%s{%s}\n\n", lit, c.elem, strings.Join(vals, ", "))
		fmt.Fprintf(f, "\tif pos+%d <= len(p.in) {\n\t\tsame := true\n\t\tfor i, v := range %s {\n\t\t\tif p.in[pos+i] != v {\n\t\t\t\tsame = false\n\t\t\t\tbreak\n\t\t\t}\n\t\t}\n\t\tif same {\n\t\t\treturn pos + %d\n\t\t}\n\t}\n", len(vals), lit, len(vals))
	}

	fmt.Fprintf(f, "\tp.fail(pos, %s)\n\treturn -1\n", quote(description(n)))
}

// emitRule writes the method of a rule: it adds a node to the tree and matches the definition.
func (c *compiler[T, P]) emitRule(name string, def *ebnf.Node[T, P]) {
	body := c.emit(def.Children[0])
	f := &c.funcs

	fmt.Fprintf(f, "func (p *\x00P\x00) %s(pos int) int {\n", c.ruleFn[name])
	fmt.Fprintf(f, "\tif p.depth >= p.maxDepth {\n\t\tp.tooDeep = true\n\t\treturn -1\n\t}\n\tp.depth++\n")
	fmt.Fprintf(f, "\ti := len(p.nodes)\n\tp.nodes = append(p.nodes, \x00N\x00{Rule: %s, Begin: int32(pos)})\n", c.ruleConst[name])

	if def.Atomic {
		fmt.Fprintf(f, "\tp.silent++\n")
	}

	fmt.Fprintf(f, "\tend := p.%s(pos)\n", body)

	if def.Atomic {
		fmt.Fprintf(f, "\tp.silent--\n")
	}

	fmt.Fprintf(f, "\tp.depth--\n\tif end < 0 {\n\t\tp.nodes = p.nodes[:i]\n")

	if def.Atomic {
		fmt.Fprintf(f, "\t\tp.fail(pos, %s)\n", quote(name))
	}

	fmt.Fprintf(f, "\t\treturn -1\n\t}\n")
	fmt.Fprintf(f, "\tp.nodes[i].End = int32(end)\n\tp.nodes[i].Size = int32(len(p.nodes) - i)\n\treturn end\n}\n\n")
}

type tmplData struct {
	Package  string
	Imports  []string
	Prefix   string
	Parser   string
	Node     string
	Tree     string
	Rule     string
	SynErr   string
	ErrDeep  string
	NewFn    string
	ParseFn  string
	Elem     string
	IsText   bool
	IsRune   bool
	MaxDepth int
	Root     string
	Rules    []ruleData
	Funcs    string
	Lits     string
}

type ruleData struct{ Const, Name string }

func (c *compiler[T, P]) generate() ([]byte, error) {
	// the rules are named before any code is written, they use each other
	for _, name := range c.g.Order {
		c.ruleConst[name] = c.unique(c.cfg.Name + "Rule" + camel(name))
		c.ruleFn[name] = c.unique("rule" + camel(name))
	}

	for _, name := range c.g.Order {
		c.emitRule(name, c.g.Rules[name])
	}

	root := c.emit(c.g.Root)

	d := tmplData{
		Package: c.cfg.Package, Imports: c.cfg.Imports, Prefix: c.cfg.Name,
		Parser: c.cfg.Name + "Parser", Node: c.cfg.Name + "Node", Tree: c.cfg.Name + "Tree", Rule: c.cfg.Name + "Rule",
		SynErr: c.cfg.Name + "SyntaxError", ErrDeep: c.cfg.Name + "ErrTooDeep",
		NewFn: "New" + c.cfg.Name + "Parser", ParseFn: c.cfg.Name + "Parse",
		Elem: c.elem, IsText: c.isText, IsRune: c.elem == "rune", MaxDepth: c.cfg.MaxDepth, Root: root,
	}

	for _, name := range c.g.Order {
		d.Rules = append(d.Rules, ruleData{Const: c.ruleConst[name], Name: name})
	}

	// the methods and the literals refer to the names of the types
	names := strings.NewReplacer("\x00P\x00", d.Parser, "\x00N\x00", d.Node, "\x00X\x00", d.Prefix)
	d.Funcs, d.Lits = names.Replace(c.funcs.String()), names.Replace(c.lits.String())

	var out bytes.Buffer
	if err := fileTemplate.Execute(&out, d); err != nil {
		return nil, err
	}

	src, err := format.Source(out.Bytes())
	if err != nil {
		return out.Bytes(), fmt.Errorf("codegen: the generated code does not parse (a bug, or a Format or Class that wrote bad Go): %w", err)
	}

	return src, nil
}
