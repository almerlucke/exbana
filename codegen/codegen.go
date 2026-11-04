// Package codegen writes a parser as Go source from a grammar made of patterns. The grammar is the specification and the
// test: design and check it with the patterns, then generate code that does the same without any of the machinery the
// patterns need to be flexible: no closures, no tree of match objects, no backtracking. The generated code does not use
// this library at all (it needs Go 1.23 or newer, for the iterators of its tree).
//
//	src, err := codegen.Generate(grammar, codegen.Config[rune, runes.Pos]{Package: "calc", Name: "Calc", UTF8: true})
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
// A rule that is left recursive, a base branch or a tail that starts with the rule itself (expr = expr "+" term | term), is
// generated as a loop: the base, then the tails for as long as they match. That is what the rule matches when it is asked for its
// longest match, if at most one base branch and one tail can match at each step and what a tail starts with cannot also start
// what follows the rule; the optimizer proves it, and the rule has to be marked with SetLeftRecursive. Left recursion in any other
// form (through other rules, or with no base branch) is refused: it would call itself without consuming anything.
//
// Leaves have to be declarative (entity.Between, In, Equals, vector.Text and the like) so that the generator can see what they
// accept; a leaf made from a function needs a Config.Class that says what it is as Go code.
//
// # The input
//
// The parser reads a slice of any element type, with an int as position. Runes, bytes, integers, strings and floats work out of
// the box. For another type, like a struct for a token, set Config.ElemType and, for the leaves that are functions,
// Config.Class. For text there is Config.UTF8: the parser reads a string (or bytes) and decodes characters as it goes, which
// takes a quarter of the memory of a slice of runes and no conversion.
//
// # What the parser returns
//
// A flat tree of the rules that matched (their names are constants) in a slice of small nodes, with methods to walk it, to find
// the nodes of a rule, to get the line and column of a position and to dump it; and a SyntaxError with the farthest failure and
// what was expected there, formatted like ebnf.FarthestLog. Evaluating the tree is up to you. A left recursive rule is one node
// that has its base and its tails as children, in order.
//
// # The typed AST
//
// Config.AST adds a type for every rule: a view of the nodes of that rule, with a method for each rule that can match inside it.
// How the method looks follows from how often the grammar lets that rule match inside the other:
//
//	exactly once        func (v Member) Value() Value
//	once or not at all  func (v Value) Number() (Number, bool)
//	any other number    func (v Object) Members() iter.Seq[Member]
//
// A rule that is one of several (value = string | number | array) also has Kind, the rule of what is inside. Tree.Top is the
// view of the top level, Tree.AsMember the view of a node that is known to be of a rule, and every view has Span, Slice and
// (for text) Text. A rule with a recovery promises nothing: it can be cut short, so its methods give a child or none, and
// Recovered says whether it was. The views hold a tree and a node number and copy nothing, so reading a document through them costs
// no more than walking the tree. The order of children of different rules is not in the methods (members of one rule are in order);
// Tree.Children has all of them in order. A rule that matches more than once inside another (pair = value ":" value) is a method that
// gives all of them; Config.Positions names them by where they are: pair.Key() and pair.Val().
//
// # Error recovery
//
// Config.Recover makes a rule survive a syntax error: ParseAll notes the error, skips to a place the grammar names, and goes on,
// so that all the errors of a document are found in one run. That is for an error inside a rule. Config.Skip is for input that
// does not start a rule where one is repeated, the garbage between two statements: ParseAll notes the error, skips to the next
// place the rule matches, and carries on with the repetition. Both leave the skipped text in the tree as nodes of the rule Error.
package codegen

import (
	"bytes"
	"fmt"
	"go/format"
	"go/token"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/optimize"
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

	// UTF8 makes the parser read text as a string of UTF-8 (Parse takes a string, ParseBytes a slice of bytes) instead of a
	// slice of runes. Positions are byte offsets; columns in errors still count characters. That is a quarter of the memory
	// and no conversion pass, and Tree.Text is a slice of the input without a copy. The elements have to be runes.
	UTF8 bool

	// AST also writes a type for every rule, a view of the nodes of that rule with a method for each rule that can match
	// inside it: Member.Value(), Object.Members(). The methods follow from how often the grammar lets a rule match inside
	// another one (once, once or not at all, any number of times), so a parse can be read without knowing the layout of the
	// tree. Tree.Top is the view of the top level. See the package documentation.
	AST bool

	// Positions names the children of a rule that matches more than once inside another one (pair = value "," value), by where
	// they are: {"pair": {"value": {"Key", "Val"}}} gives pair.Key() and pair.Val(), the first and second value inside a pair.
	// The names have to be exported identifiers and are only used with AST. A name is a method that gives the child if the
	// grammar always has it there, and a child and ok if it may not.
	Positions map[string]map[string][]string

	// Skip says, for rules that are repeated, what to do with input that is not one of them: see Skip. The keys are rule names.
	Skip map[string]Skip[T, P]

	// Recover says, for rules, how to go on after a syntax error in them: see Recovery. The keys are rule names.
	Recover map[string]Recovery[T, P]

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

// Recovery makes a rule survive a syntax error. When the rule fails after it has matched something, the parser notes the error,
// skips elements until the sync pattern matches, and treats the rule as matched: it spans the skipped text and has a child
// node of the rule Error where the text was skipped. ParseAll, which collects all the errors, is the only parse that does this;
// Parse stops at the first error as before.
//
// A rule that fails on its very first element does not recover: that is how a repetition of it ends, not an error in it.
// Garbage between two rules is for the grammar to skip, with an alternative that takes it.
type Recovery[T, P any] struct {
	// Until is where to go on: elements are skipped one at a time, starting where the parse got stuck, until it matches. It must
	// be a pattern of the kind the generator accepts, without rules. At the end of the input the skipping stops.
	Until ebnf.Pattern[T, P]

	// Consume makes the match of Until part of the recovered rule (the ";" that ends a statement). Otherwise the parse goes on
	// at the match, which is then for the rest of the grammar to take (a "}" that closes a block).
	Consume bool
}

// Skip makes a repetition of a rule survive input that does not start the rule, the garbage between two statements. It works in
// ParseAll, for a rule that is the item of a repetition that has no upper limit (a rule is repeated as { statement }).
// When the rule fails at a place where the repetition does not end, the parser notes the syntax error, skips elements until
// the rule matches again (or the repetition ends, or the input does), puts the skipped text in a node of the rule Error in the
// tree, and goes on with the repetition. That is also what happens when the rule fails further on and has no Recover of its own.
//
// Where a repetition ends is a matter of what follows it, which the generator does not work out: the end of the input always
// ends it, and Stop says where else (the "}" of a block). If Stop is missing for a repetition that ends before something other
// than the end of the input, that something is taken for garbage and skipped.
type Skip[T, P any] struct {
	// Stop is where the repetition ends, other than the end of the input. It is only looked at, not consumed. It must be a
	// pattern of the kind the generator accepts, without rules, and can be nil.
	Stop ebnf.Pattern[T, P]

	// Until is where to go on: elements are skipped until it matches there, and the repetition continues at the match. The
	// default is nil, which is where the rule itself matches. If given it must be a pattern without rules.
	Until ebnf.Pattern[T, P]
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
	c.root = p

	c.validate()

	if len(c.problems) > 0 {
		return nil, &Error{Problems: c.problems}
	}

	return c.generate()
}

type compiler[T, P any] struct {
	g   *ebnf.Grammar[T, P]
	cfg Config[T, P]

	root ebnf.Pattern[T, P]

	elem   string
	isText bool // runes and bytes: errors have a line and a column, and nodes have text
	utf8   bool // the input is a string of UTF-8, and the elements runes decoded from it

	problems []string
	untils   map[string]*ebnf.Node[T, P] // the Until pattern of every rule that has a recovery
	skipStop map[string]*ebnf.Node[T, P] // the Stop and Until patterns of the rules that have a Skip, if they have one
	skipTo   map[string]*ebnf.Node[T, P]

	// left recursive rules that are generated as a loop, and what the optimizer found about each of them
	lr    map[string]lrSplit[T, P]
	loops map[string]optimize.Decision[T, P]

	// names
	used        map[string]bool
	ruleFn      map[string]string
	ruleConst   map[string]string
	ruleIdent   map[string]string
	ruleRecover map[string]string
	ruleSkip    map[string]string
	viewType    map[string]string // the view type of every rule, with Config.AST
	topType     string
	counter     int

	funcs strings.Builder
	lits  strings.Builder
}

type lrSplit[T, P any] struct {
	bases, tails []*ebnf.Node[T, P]
}

func newCompiler[T, P any](g *ebnf.Grammar[T, P], cfg Config[T, P]) *compiler[T, P] {
	c := &compiler[T, P]{
		g: g, cfg: cfg, lr: map[string]lrSplit[T, P]{}, loops: map[string]optimize.Decision[T, P]{},
		used:   map[string]bool{},
		ruleFn: map[string]string{}, ruleConst: map[string]string{}, ruleIdent: map[string]string{}, ruleRecover: map[string]string{}, ruleSkip: map[string]string{}, viewType: map[string]string{},
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

	c.utf8 = cfg.UTF8
	if c.utf8 {
		c.elem = "rune"
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
	if c.utf8 && reflect.TypeFor[T]().Kind() != reflect.Int32 {
		c.problem("Config.UTF8 reads text, the elements of the grammar have to be runes and not %s", reflect.TypeFor[T]())
		return
	}

	c.validateNode(c.g.Root, "")

	for _, name := range c.g.Order {
		def := c.g.Rules[name]

		bases, tails, err := c.g.SplitLeftRecursive(name)

		switch {
		case def.LeftRecursive && err != nil:
			c.problem("rule %s is marked left recursive but %v (the generator turns a rule of the form base | rule tail into a loop)", name, err)
			c.validateNode(def.Children[0], " in "+name)
		case err == nil && len(tails) > 0 && !def.LeftRecursive:
			c.problem("rule %s is left recursive and not marked: use SetLeftRecursive on it, the generator turns it into a loop", name)
		case def.LeftRecursive && len(tails) > 0:
			// a loop: the bases and what the tails add after the rule itself are what is generated
			c.lr[name] = lrSplit[T, P]{bases, tails}

			for _, b := range bases {
				c.validateNode(b, " in "+name)
			}

			for _, t := range tails {
				for _, part := range t.Children[1:] {
					c.validateNode(part, " in "+name)
				}
			}
		default:
			c.validateNode(def.Children[0], " in "+name)
		}
	}

	// a rule that can call itself before it has consumed anything would never stop
	for _, name := range c.g.LeftCycles() {
		c.problem("rule %s can call itself without consuming anything, directly or through other rules (left recursion: write it as a loop)", name)
	}

	c.proveLoops()

	c.untils = map[string]*ebnf.Node[T, P]{}

	for name, rec := range c.cfg.Recover {
		if _, ok := c.g.Rules[name]; !ok {
			c.problem("Config.Recover names a rule that the grammar does not have: %s", name)
			continue
		}

		if rec.Until == nil {
			c.problem("the recovery of %s has no Until pattern", name)
			continue
		}

		if root := c.sidePattern(rec.Until, "the recovery of "+name); root != nil {
			c.untils[name] = root
		}
	}

	c.validateSkip()
	c.validatePositions()
}

// sidePattern analyzes a pattern of the configuration, which has to stand on its own, and returns nil if it is not usable.
func (c *compiler[T, P]) sidePattern(p ebnf.Pattern[T, P], what string) *ebnf.Node[T, P] {
	ug, err := ebnf.Analyze(p)
	if err != nil {
		c.problem("%s: %v", what, err)
		return nil
	}

	if len(ug.Rules) > 0 {
		c.problem("%s uses rules: it has to be a pattern without them", what)
		return nil
	}

	c.validateNode(ug.Root, " in "+what)

	return ug.Root
}

func (c *compiler[T, P]) validateSkip() {
	c.skipStop, c.skipTo = map[string]*ebnf.Node[T, P]{}, map[string]*ebnf.Node[T, P]{}

	for name, s := range c.cfg.Skip {
		if _, ok := c.g.Rules[name]; !ok {
			c.problem("Config.Skip names a rule that the grammar does not have: %s", name)
			continue
		}

		if s.Stop != nil {
			if root := c.sidePattern(s.Stop, "the Stop of the skip of "+name); root != nil {
				c.skipStop[name] = root
			}
		}

		if s.Until != nil {
			if root := c.sidePattern(s.Until, "the Until of the skip of "+name); root != nil {
				c.skipTo[name] = root
			}
		}
	}
}

// reservedMethods are the methods every view has.
var reservedMethods = []string{"Tree", "Node", "Kind", "Span", "Slice", "Text", "Recovered"}

func (c *compiler[T, P]) validatePositions() {
	if len(c.cfg.Positions) == 0 {
		return
	}

	if !c.cfg.AST {
		c.problem("Config.Positions names methods of the views, it needs Config.AST")
		return
	}

	for name, byRule := range c.cfg.Positions {
		if _, ok := c.g.Rules[name]; !ok {
			c.problem("Config.Positions names a rule that the grammar does not have: %s", name)
			continue
		}

		set := c.childrenOfRule(name)
		used := map[string]string{}

		for child, names := range byRule {
			cd, ok := set.cards[child]
			if !ok {
				c.problem("Config.Positions: %s does not have %s inside", name, child)
				continue
			}

			if len(names) > cd.max {
				c.problem("Config.Positions: %s has %s inside at most %d times, and %d names are given", name, child, cd.max, len(names))
			}

			for _, n := range names {
				switch {
				case !token.IsIdentifier(n) || !token.IsExported(n):
					c.problem("Config.Positions: %q (in %s) is not an exported identifier", n, name)
				case slices.Contains(reservedMethods, n):
					c.problem("Config.Positions: %s (in %s) is a method every view has", n, name)
				case used[n] != "":
					c.problem("Config.Positions: %s is used twice in %s (for %s and %s)", n, name, used[n], child)
				default:
					used[n] = child
				}
			}
		}
	}
}

// proveLoops asks the optimizer whether the left recursive rules can be evaluated as a loop: a base and then tails, for as
// long as they match, is what the rules offer as their longest match when at most one thing can match at every step and
// what a tail starts with can not be what follows the rule. The parser has nothing after its root, so the grammar is
// closed. Config.Commit takes the answer for granted.
func (c *compiler[T, P]) proveLoops() {
	if len(c.lr) == 0 || c.cfg.Commit {
		return
	}

	res, err := optimize.AnalyzeFunc(c.root, optimize.Options{Closed: true}, c.conv())
	if err != nil {
		c.problem("%v", err)
		return
	}

	for _, d := range res.Decisions {
		if d.Kind != optimize.Loop {
			continue
		}

		c.loops[d.Rule] = d

		if _, generated := c.lr[d.Rule]; generated && !d.Safe {
			c.problem("rule %s is left recursive and can not be shown to match like a loop: %s (set Config.Commit to generate the loop anyway)", d.Rule, d.Reason)
		}
	}
}

// conv maps an element to an integer for the analysis, if the elements are integers.
func (c *compiler[T, P]) conv() func(T) int64 {
	switch reflect.TypeFor[T]().Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return func(v T) int64 { return reflect.ValueOf(v).Int() }
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return func(v T) int64 { return int64(reflect.ValueOf(v).Uint()) }
	}

	return nil
}

// validateNode finds what can not be generated in a node and in the nodes below it. where says where the node is, for
// the messages.
func (c *compiler[T, P]) validateNode(n *ebnf.Node[T, P], where string) {
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

				// a literal is compared as bytes, so it has to be valid UTF-8, and U+FFFD would also match a byte that is not
				if r := reflect.ValueOf(v); c.utf8 && (!utf8.ValidRune(rune(r.Int())) || rune(r.Int()) == utf8.RuneError) {
					c.problem("a literal%s has a character that can not be compared as UTF-8 (U+FFFD or not a character): %s", where, n.String())
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
		c.validateNode(ch, where)
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

// sliceType is the Go type of a part of the input.
func (c *compiler[T, P]) sliceType() string {
	if c.utf8 {
		return "string"
	}

	return "[]" + c.elem
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
	switch n.Kind {
	case ebnf.KindRef:
		return c.ruleFn[n.ID]
	case ebnf.KindSeq:
		return c.emitSeq(c.emitAll(n.Children))
	case ebnf.KindAlt:
		return c.emitAlt(c.emitAll(n.Children), n.ID)
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
		cond := c.classCond(n)

		// the element is only read when the test looks at it (any element, or none, needs no test)
		read, width := "", "1"

		switch {
		case !strings.Contains(cond, "x"):
		case c.utf8:
			read, width = "\t\tx, w := p.rune(pos)\n", "w"
		default:
			read = "\t\tx := p.in[pos]\n"
		}

		if c.utf8 && !strings.Contains(cond, "x") {
			read, width = "\t\t_, w := p.rune(pos)\n", "w"
		}

		fmt.Fprintf(f, "\tif pos < len(p.in) {\n%s\t\tif %s {\n\t\t\treturn pos + %s\n\t\t}\n\t}\n", read, cond, width)
		fmt.Fprintf(f, "\tp.fail(pos, %s)\n\treturn -1\n", quote(description(n)))
	case ebnf.KindEnd:
		fmt.Fprintf(f, "\tif pos == len(p.in) {\n\t\treturn pos\n\t}\n\tp.fail(pos, %s)\n\treturn -1\n", quote(description(n)))
	case ebnf.KindRepeat:
		item := c.emit(n.Children[0])

		fmt.Fprintf(f, "\tq, n := pos, 0\n")

		if n.Max > 0 {
			fmt.Fprintf(f, "\tfor n < %d {\n", n.Max)
		} else {
			fmt.Fprintf(f, "\tfor {\n")
		}

		// garbage between the items is skipped, in a repetition of a rule that says so
		skip := ""

		if ch := n.Children[0]; n.Max == 0 && ch.Kind == ebnf.KindRef {
			if _, ok := c.cfg.Skip[ch.ID]; ok {
				skip = fmt.Sprintf("\t\t\tif s := p.%s(q); s > q {\n\t\t\t\tq = s\n\t\t\t\tcontinue\n\t\t\t}\n", c.ruleSkip[ch.ID])
			}
		}

		fmt.Fprintf(f, "\t\tmark := len(p.nodes)\n\t\tr := p.%s(q)\n\t\tif r < 0 {\n\t\t\tp.nodes = p.nodes[:mark]\n%s\t\t\tbreak\n\t\t}\n", item, skip)
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

// emitAll writes the methods of the nodes and returns their names.
func (c *compiler[T, P]) emitAll(nodes []*ebnf.Node[T, P]) []string {
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = c.emit(n)
	}

	return names
}

// emitSeq writes the method that matches the methods one after the other.
func (c *compiler[T, P]) emitSeq(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}

	name := c.next("m")
	f := &c.funcs

	fmt.Fprintf(f, "func (p *\x00P\x00) %s(pos int) int {\n\tq := pos\n", name)

	for _, part := range parts {
		fmt.Fprintf(f, "\tif q = p.%s(q); q < 0 {\n\t\treturn -1\n\t}\n", part)
	}

	fmt.Fprintf(f, "\treturn q\n}\n\n")

	return name
}

// emitAlt writes the method that matches the first of the methods that matches. If the alternation has a name, a failure
// is reported with it.
func (c *compiler[T, P]) emitAlt(parts []string, id string) string {
	if len(parts) == 1 && id == "" {
		return parts[0]
	}

	name := c.next("m")
	f := &c.funcs

	fmt.Fprintf(f, "func (p *\x00P\x00) %s(pos int) int {\n\tmark := len(p.nodes)\n", name)

	for _, part := range parts {
		fmt.Fprintf(f, "\tif q := p.%s(pos); q >= 0 {\n\t\treturn q\n\t}\n\tp.nodes = p.nodes[:mark]\n", part)
	}

	if id != "" {
		fmt.Fprintf(f, "\tp.fail(pos, %s)\n", quote(id))
	}

	fmt.Fprintf(f, "\treturn -1\n}\n\n")

	return name
}

// emitLoop writes the method for a left recursive rule: a base branch, then tails for as long as they match.
func (c *compiler[T, P]) emitLoop(lr lrSplit[T, P]) string {
	base := c.emitAlt(c.emitAll(lr.bases), "")

	tails := make([]string, len(lr.tails))
	for i, t := range lr.tails {
		// the first part of a tail is the rule itself, which the loop stands for
		tails[i] = c.emitSeq(c.emitAll(t.Children[1:]))
	}

	tail := c.emitAlt(tails, "")

	name := c.next("m")
	f := &c.funcs

	fmt.Fprintf(f, "func (p *\x00P\x00) %s(pos int) int {\n\tq := p.%s(pos)\n\tif q < 0 {\n\t\treturn -1\n\t}\n", name, base)
	fmt.Fprintf(f, "\tfor {\n\t\tmark := len(p.nodes)\n\t\te := p.%s(q)\n\t\tif e <= q {\n\t\t\tp.nodes = p.nodes[:mark]\n\t\t\tbreak\n\t\t}\n\t\tq = e\n\t}\n\treturn q\n}\n\n", tail)

	return name
}

func (c *compiler[T, P]) emitLiteral(f *strings.Builder, n *ebnf.Node[T, P], name string) {
	if c.utf8 {
		runes := make([]rune, len(n.Values))
		for i, v := range n.Values {
			runes[i] = rune(reflect.ValueOf(v).Int())
		}

		lit := string(runes)

		if len(lit) == 0 {
			fmt.Fprintf(f, "\treturn pos\n")
			return
		}

		fmt.Fprintf(f, "\tif len(p.in)-pos >= %d && p.in[pos:pos+%d] == %s {\n\t\treturn pos + %d\n\t}\n", len(lit), len(lit), strconv.Quote(lit), len(lit))
		fmt.Fprintf(f, "\tp.fail(pos, %s)\n\treturn -1\n", quote(description(n)))

		return
	}

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
	var body string

	if lr, ok := c.lr[name]; ok {
		body = c.emitLoop(lr)
	} else {
		body = c.emit(def.Children[0])
	}

	rec, recovers := c.cfg.Recover[name]
	if recovers {
		c.emitRecovery(name, rec)
	}

	if _, skips := c.cfg.Skip[name]; skips {
		c.emitSkip(name)
	}

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

	fmt.Fprintf(f, "\tp.depth--\n")

	if recovers {
		// failing after something matched is a syntax error in this rule; failing at its first element is not (it is how a
		// repetition of the rule ends)
		fmt.Fprintf(f, "\tif end < 0 && p.recovering && p.silent == 0 && p.errPos > pos {\n\t\tend = p.%s(pos, i)\n\t}\n", c.ruleRecover[name])
	}

	fmt.Fprintf(f, "\tif end < 0 {\n\t\tp.nodes = p.nodes[:i]\n")

	if def.Atomic {
		fmt.Fprintf(f, "\t\tp.fail(pos, %s)\n", quote(name))
	}

	fmt.Fprintf(f, "\t\treturn -1\n\t}\n")
	fmt.Fprintf(f, "\tp.nodes[i].End = int32(end)\n\tp.nodes[i].Size = int32(len(p.nodes) - i)\n\treturn end\n}\n\n")
}

// emitRecovery writes the method that skips to where the parse can go on after a syntax error in the rule.
func (c *compiler[T, P]) emitRecovery(name string, rec Recovery[T, P]) {
	sync := c.emit(c.untils[name])
	f := &c.funcs

	step := "s++"
	if c.utf8 {
		step = "_, w := p.rune(s)\n\t\ts += w"
	}

	fmt.Fprintf(f, "// %s notes the syntax error in the rule that began at pos, and skips to where the parse can go on. i is the node of the rule.\n", c.ruleRecover[name])
	fmt.Fprintf(f, "func (p *\x00P\x00) %s(pos, i int) int {\n", c.ruleRecover[name])
	fmt.Fprintf(f, "\tp.errors = append(p.errors, p.syntaxError())\n")
	fmt.Fprintf(f, "\ts := p.errPos\n\tp.errPos, p.expected = -1, p.expected[:0]\n")
	fmt.Fprintf(f, "\tp.nodes = p.nodes[:i+1]\n")
	fmt.Fprintf(f, "\tfor s < len(p.in) {\n\t\tp.silent++\n\t\te := p.%s(s)\n\t\tp.silent--\n\t\tp.nodes = p.nodes[:i+1]\n", sync)

	if rec.Consume {
		fmt.Fprintf(f, "\t\tif e >= 0 {\n\t\t\ts = e\n\t\t\tbreak\n\t\t}\n")
	} else {
		fmt.Fprintf(f, "\t\tif e >= 0 {\n\t\t\tbreak\n\t\t}\n")
	}

	fmt.Fprintf(f, "\t\t%s\n\t}\n", step)
	fmt.Fprintf(f, "\tp.nodes = append(p.nodes, \x00N\x00{Rule: %s, Begin: int32(pos), End: int32(s), Size: 1})\n\treturn s\n}\n\n", c.errorConst())
}

// emitSkip writes the method that skips garbage where a repetition of the rule can not go on.
func (c *compiler[T, P]) emitSkip(name string) {
	// the methods of the patterns are written before this one, they must not end up in the middle of it
	stopFn, toFn := "", c.ruleFn[name]
	if stop, ok := c.skipStop[name]; ok {
		stopFn = c.emit(stop)
	}

	if to, ok := c.skipTo[name]; ok {
		toFn = c.emit(to)
	}

	f := &c.funcs
	fn := c.ruleSkip[name]

	step := "s++"
	if c.utf8 {
		step = "_, w := p.rune(s)\n\t\ts += w"
	}

	fmt.Fprintf(f, "// %s is for a repetition of %s that can not go on at pos. Unless that is where the repetition ends, and when the parse\n// is recovering, it notes the syntax error and skips to where the repetition can go on. It returns where that is, or pos.\n", fn, name)
	fmt.Fprintf(f, "func (p *\x00P\x00) %s(pos int) int {\n\tif !p.recovering || p.silent > 0 || pos >= len(p.in) {\n\t\treturn pos\n\t}\n\n", fn)

	// the end of the input is a place to stop; whatever else Stop accepts as well. Looking for it, not silently, is also how
	// the error comes to say what was expected
	mark := "\tmark := len(p.nodes)\n"
	if stopFn != "" {
		fmt.Fprintf(f, "%s\tif p.%s(pos) >= 0 {\n\t\tp.nodes = p.nodes[:mark]\n\t\treturn pos\n\t}\n\tp.nodes = p.nodes[:mark]\n", mark, stopFn)
	}

	fmt.Fprintf(f, "\tp.fail(pos, \"end of input\")\n\tp.errors = append(p.errors, p.syntaxError())\n\tp.errPos, p.expected = -1, p.expected[:0]\n\n")
	fmt.Fprintf(f, "\ts := pos\n\tfor {\n\t\t%s\n\t\tif s >= len(p.in) {\n\t\t\ts = len(p.in)\n\t\t\tbreak\n\t\t}\n\n", step)
	fmt.Fprintf(f, "\t\tmark := len(p.nodes)\n\t\tp.silent++\n\t\thit := false\n")

	if stopFn != "" {
		fmt.Fprintf(f, "\t\thit = p.%s(s) >= 0\n", stopFn)
	}

	fmt.Fprintf(f, "\t\thit = hit || p.%s(s) >= 0\n", toFn)

	fmt.Fprintf(f, "\t\tp.silent--\n\t\tp.nodes = p.nodes[:mark]\n\t\tif hit {\n\t\t\tbreak\n\t\t}\n\t}\n\n")
	fmt.Fprintf(f, "\tp.nodes = append(p.nodes, \x00N\x00{Rule: %s, Begin: int32(pos), End: int32(s), Size: 1})\n\treturn s\n}\n\n", c.errorConst())
}

func (c *compiler[T, P]) errorConst() string {
	return c.cfg.Name + "RuleError"
}

type tmplData struct {
	Package   string
	Imports   []string
	Prefix    string
	Parser    string
	Node      string
	Tree      string
	Rule      string
	SynErr    string
	ErrDeep   string
	NewFn     string
	ParseFn   string
	Elem      string
	In        string
	SliceType string
	UTF8      bool
	Recover   bool
	ErrorRule string
	IsText    bool
	IsRune    bool
	MaxDepth  int
	Root      string
	Rules     []ruleData
	Funcs     string
	Views     string
	Lits      string
}

type ruleData struct{ Const, Name, Ident string }

func (c *compiler[T, P]) generate() ([]byte, error) {
	if c.cfg.AST {
		// the views are named after the rules, which must not take the names of the other types
		for _, n := range []string{"Parser", "Tree", "Node", "Rule", "SyntaxError", "ErrTooDeep", "Parse"} {
			c.used[c.cfg.Name+n] = true
		}

		c.used["New"+c.cfg.Name+"Parser"] = true
	}

	// the rules are named before any code is written, they use each other
	idents := map[string]bool{}

	for _, name := range c.g.Order {
		ident := camel(name)
		for i := 2; idents[ident]; i++ {
			ident = camel(name) + strconv.Itoa(i)
		}

		idents[ident] = true
		c.ruleIdent[name] = ident
		c.ruleConst[name] = c.unique(c.cfg.Name + "Rule" + ident)
		c.ruleFn[name] = c.unique("rule" + ident)
		c.ruleRecover[name] = c.unique("recover" + ident)
		c.ruleSkip[name] = c.unique("skip" + ident)

		if c.cfg.AST {
			c.viewType[name] = c.unique(c.cfg.Name + ident)
		}
	}

	if c.cfg.AST {
		c.topType = c.unique(c.cfg.Name + "Top")
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
		Elem: c.elem, IsText: c.isText, IsRune: c.elem == "rune" && !c.utf8, UTF8: c.utf8, MaxDepth: c.cfg.MaxDepth, Root: root,
		In: "[]" + c.elem, SliceType: "[]" + c.elem,
		Recover: len(c.cfg.Recover)+len(c.cfg.Skip) > 0, ErrorRule: c.errorConst(),
	}

	if c.utf8 {
		d.In, d.SliceType = "string", "string"
	}

	for _, name := range c.g.Order {
		d.Rules = append(d.Rules, ruleData{Const: c.ruleConst[name], Name: name, Ident: c.ruleIdent[name]})
	}

	// the methods and the literals refer to the names of the types
	names := strings.NewReplacer("\x00P\x00", d.Parser, "\x00N\x00", d.Node, "\x00X\x00", d.Prefix)
	d.Funcs, d.Lits = names.Replace(c.funcs.String()), names.Replace(c.lits.String())

	if c.cfg.AST {
		d.Views = names.Replace(c.emitViews())
	}

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
