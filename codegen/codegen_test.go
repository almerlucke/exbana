package codegen_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/codegen"
	"github.com/almerlucke/exbana/v3/internal/gentest"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/except"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

type (
	pat = ebnf.Pattern[rune, runes.Pos]
	cfg = codegen.Config[rune, runes.Pos]
)

func between(lo, hi rune) *entity.Entity[rune, runes.Pos] {
	return entity.Between[rune, runes.Pos](lo, hi)
}
func text(s string) *vector.Vector[rune, runes.Pos] { return vector.Text[runes.Pos](s) }
func conc(ps ...pat) *concatenation.Concatenation[rune, runes.Pos] {
	return concatenation.New[rune, runes.Pos](ps...)
}
func alt(ps ...pat) *alternation.Alternation[rune, runes.Pos] {
	return alternation.New[rune, runes.Pos](ps...)
}
func many(p pat) *repetition.Repetition[rune, runes.Pos] { return repetition.Any[rune, runes.Pos](p) }
func some(p pat) *repetition.Repetition[rune, runes.Pos] {
	return repetition.OneOrMore[rune, runes.Pos](p)
}
func maybe(p pat) *repetition.Repetition[rune, runes.Pos] {
	return repetition.Optional[rune, runes.Pos](p)
}

// calc = number { ("+" | "-") number } end, with the backtracking taken out by the optimizer
func calc(t testing.TB) pat {
	t.Helper()

	number := rule.New[rune, runes.Pos]("number", some(between('0', '9').Printed("digit")))
	sum := rule.New[rune, runes.Pos]("sum", conc(number, many(conc(alt(text("+"), text("-")), number)), end.New[rune, runes.Pos]().Printed("end of input")))

	if _, err := optimize.Optimize[rune, runes.Pos](sum, optimize.Options{}); err != nil {
		t.Fatal(err)
	}

	return sum
}

func problems(t *testing.T, err error) []string {
	t.Helper()

	var e *codegen.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want a *codegen.Error", err)
	}

	return e.Problems
}

func TestRefusesWhatItCanNotGenerateAndSaysEverythingAtOnce(t *testing.T) {
	// a repetition and an alternation that backtrack, a function as a leaf, and a pattern from scratch
	grammar := conc(
		many(text("a")),
		alt(text("b"), text("bc")),
		entity.New[rune, runes.Pos](func(r rune) bool { return r == 'x' }).Named("x"),
		scratch{},
	)

	_, err := codegen.Generate[rune, runes.Pos](grammar, cfg{Package: "p"})

	got := strings.Join(problems(t, err), "\n")
	for _, want := range []string{"a repetition that backtracks", "an alternation that backtracks", "a class decided by a function", "a pattern that can not be looked into"} {
		if !strings.Contains(got, want) {
			t.Errorf("the problems do not mention %q:\n%s", want, got)
		}
	}

	if n := len(problems(t, err)); n != 4 {
		t.Errorf("%d problems, want 4:\n%s", n, got)
	}
}

type scratch struct{}

func (scratch) MatchCont(ebnf.Reader[rune, runes.Pos], ebnf.Continuation[rune, runes.Pos]) (bool, error) {
	return false, nil
}
func (scratch) Generate(*ebnf.GenContext, ebnf.Writer[rune]) error { return nil }
func (scratch) Print(w io.Writer) error                            { return nil }

func TestRefusesInexactLiterals(t *testing.T) {
	fold := vector.New[rune, runes.Pos](func(a, b rune) bool { return a == b }, 'a', 'b')

	_, err := codegen.Generate[rune, runes.Pos](fold, cfg{Package: "p"})
	if got := strings.Join(problems(t, err), "\n"); !strings.Contains(got, "compared with a function") {
		t.Errorf("problems:\n%s", got)
	}
}

// expr = expr ("+" | "-") term | term, term = term ("*" | "/") factor | factor, factor = number | "(" expr ")"
func arithmetic(t *testing.T) pat {
	t.Helper()

	number := rule.New[rune, runes.Pos]("number", some(between('0', '9')))
	expr, term := rule.Declare[rune, runes.Pos]("expr").SetLeftRecursive(true), rule.Declare[rune, runes.Pos]("term").SetLeftRecursive(true)
	factor := rule.Declare[rune, runes.Pos]("factor")
	add := rule.New[rune, runes.Pos]("add", alt(text("+"), text("-")))
	mul := rule.New[rune, runes.Pos]("mul", alt(text("*"), text("/")))

	expr.Define(alt(conc(expr, add, term), term))
	term.Define(alt(conc(term, mul, factor), factor))
	factor.Define(alt(number, conc(text("("), expr, text(")"))))

	input := rule.New[rune, runes.Pos]("input", conc(expr, end.New[rune, runes.Pos]().Printed("end of input")))

	if res, err := optimize.Optimize[rune, runes.Pos](input, optimize.Options{Closed: true}); err != nil || len(res.Blocked()) != 0 {
		t.Fatalf("%v\n%v", err, res)
	}

	return input
}

func TestLeftRecursionIsGeneratedAsALoop(t *testing.T) {
	var files = map[string]string{}

	for _, utf8 := range []bool{false, true} {
		name := map[bool]string{false: "R", true: "U"}[utf8]

		src, err := codegen.Generate[rune, runes.Pos](arithmetic(t), cfg{Package: "probe", Name: name, UTF8: utf8})
		if err != nil {
			t.Fatal(err)
		}

		files[strings.ToLower(name)+".go"] = string(src)
	}

	files["calc_test.go"] = `package probe

import (
	"strconv"
	"strings"
	"testing"
)

// eval walks the tree: a left recursive rule is a node with its parts as children, in order, which is the order of
// evaluation, so the operators group to the left
func eval(t *testing.T, tree *RTree, node int) int {
	var children []int
	for c := range tree.Children(node) {
		children = append(children, c)
	}

	switch tree.Rule(node) {
	case RRuleNumber:
		n, _ := strconv.Atoi(tree.Text(node))
		return n
	case RRuleFactor:
		if first, ok := tree.FirstNumber(node); ok {
			return eval(t, tree, first)
		}
		first, _ := tree.FirstExpr(node)
		return eval(t, tree, first)
	case RRuleExpr, RRuleTerm:
		v := eval(t, tree, children[0])
		for i := 1; i < len(children); i += 2 {
			r := eval(t, tree, children[i+1])
			switch tree.Text(children[i]) {
			case "+":
				v += r
			case "-":
				v -= r
			case "*":
				v *= r
			case "/":
				v /= r
			}
		}
		return v
	}

	t.Fatalf("rule %v", tree.Rule(node))

	return 0
}

func TestArithmetic(t *testing.T) {
	p := NewRParser()

	for in, want := range map[string]int{
		"7": 7, "1+2": 3, "1+2*3": 7, "(1+2)*3": 9, "10-3-2": 5, "100/10/5": 2, "2*3+4*5": 26, "1-2-3-4": -8,
		"((((1))))": 1, "2*(3+(4-1))": 12, "8/4/2*3-1": 2,
	} {
		tree, err := p.ParseString(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}

		top, _ := tree.FirstExpr(0)
		if got := eval(t, tree, top); got != want {
			t.Errorf("%q = %d, want %d", in, got, want)
		}
	}

	// one node for the whole chain, with the terms and the operators as its children
	tree, _ := p.ParseString("1+2*3-4")
	if got := strings.Split(strings.TrimSpace(tree.Dump()), "\n")[1]; got != "  expr 0-7 \"1+2*3-4\"" {
		t.Errorf("the expression: %s", got)
	}

	var parts []string
	top, _ := tree.FirstExpr(0)
	for c := range tree.Children(top) {
		parts = append(parts, tree.Rule(c).String()+"="+tree.Text(c))
	}
	if strings.Join(parts, " ") != "term=1 add=+ term=2*3 add=- term=4" {
		t.Errorf("parts: %v", parts)
	}

	// the same in text mode
	if tree, err := NewUParser().Parse("(1+2)*3"); err != nil || tree.End != 7 {
		t.Errorf("text: %v %v", tree, err)
	}

	for in, want := range map[string]string{
		"":      "syntax error at 1:1: unexpected end of input",
		"1+":    "syntax error at 1:3: unexpected end of input",
		"1+*":   "syntax error at 1:3: unexpected '*'",
		"(1+2":  "syntax error at 1:5: unexpected end of input",
		"1 + 2": "syntax error at 1:2: unexpected ' '",
	} {
		_, err := p.ParseString(in)
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%q: %v, want %q", in, err, want)
		}
	}
}
`

	gentest.Run(t, files)
}

func TestLeftRecursionThatCanNotBeALoopIsRefused(t *testing.T) {
	x, y := text("x"), text("y")

	define := func(d func(r *rule.Rule[rune, runes.Pos]) pat, marked bool) pat {
		r := rule.Declare[rune, runes.Pos]("r").SetLeftRecursive(marked)
		r.Define(d(r))

		return r
	}

	// r = r "x" | "y"
	simple := func(r *rule.Rule[rune, runes.Pos]) pat { return alt(conc(r, x), y) }

	for name, c := range map[string]struct {
		grammar pat
		want    string
	}{
		"not marked":      {define(simple, false), "left recursive and not marked"},
		"no base":         {define(func(r *rule.Rule[rune, runes.Pos]) pat { return conc(r, x) }, true), "nothing for it to start from"},
		"tail may follow": {conc(define(simple, true), text("xz")), "what a tail starts with and what follows the rule"},
		"tail matches nothing": {define(func(r *rule.Rule[rune, runes.Pos]) pat {
			return alt(conc(r, many(x).SetPossessive(true)), y)
		}, true), "can match nothing"},
		"tails start the same": {define(func(r *rule.Rule[rune, runes.Pos]) pat {
			return alt(conc(r, x, y), conc(r, x), y)
		}, true), "tails 1 and 2"},
	} {
		_, err := codegen.Generate[rune, runes.Pos](c.grammar, cfg{Package: "p"})
		if got := strings.Join(problems(t, err), "\n"); !strings.Contains(got, c.want) {
			t.Errorf("%s: problems:\n%s", name, got)
		}
	}

	// the loop is generated when it is asked for, whatever the analysis says
	if _, err := codegen.Generate[rune, runes.Pos](conc(define(simple, true), text("xz")), cfg{Package: "p", Commit: true}); err != nil {
		t.Errorf("Commit: %v", err)
	}

	// left recursion through another rule can not be a loop
	p, q := rule.Declare[rune, runes.Pos]("p"), rule.Declare[rune, runes.Pos]("q")
	p.Define(alt(conc(q, x), y))
	q.Define(conc(p, text("z")))

	_, err := codegen.Generate[rune, runes.Pos](p, cfg{Package: "p", Commit: true})
	if got := strings.Join(problems(t, err), "\n"); !strings.Contains(got, "can call itself without consuming anything") {
		t.Errorf("indirect: problems:\n%s", got)
	}
}

func TestPackageIsRequiredAndGrammarErrorsArePassedOn(t *testing.T) {
	if _, err := codegen.Generate[rune, runes.Pos](text("a"), cfg{}); err == nil {
		t.Error("expected an error without a package")
	}

	if _, err := codegen.Generate[rune, runes.Pos](rule.Declare[rune, runes.Pos]("later"), cfg{Package: "p"}); !errors.Is(err, ebnf.ErrUndefinedRule) {
		t.Errorf("err = %v", err)
	}
}

func TestCommitGeneratesForGrammarsThatBacktrack(t *testing.T) {
	grammar := conc(many(text("a")), alt(text("b"), text("bc")))

	if _, err := codegen.Generate[rune, runes.Pos](grammar, cfg{Package: "p"}); err == nil {
		t.Fatal("expected a refusal")
	}

	if _, err := codegen.Generate[rune, runes.Pos](grammar, cfg{Package: "p", Commit: true}); err != nil {
		t.Errorf("Commit: %v", err)
	}
}

func TestOutputIsFormattedStableAndMarkedAsGenerated(t *testing.T) {
	a, err := codegen.Generate[rune, runes.Pos](calc(t), cfg{Package: "calc", Name: "Calc"})
	if err != nil {
		t.Fatal(err)
	}

	b, _ := codegen.Generate[rune, runes.Pos](calc(t), cfg{Package: "calc", Name: "Calc"})
	if !bytes.Equal(a, b) {
		t.Error("two runs give different code")
	}

	if !bytes.HasPrefix(a, []byte("// Code generated by exbana/codegen. DO NOT EDIT.")) {
		t.Error("the file is not marked as generated")
	}

	if bytes.Contains(a, []byte("exbana")) && bytes.Contains(a, []byte("github.com/almerlucke")) {
		t.Error("the generated code refers to the library")
	}
}

func TestGeneratedCalculator(t *testing.T) {
	src, err := codegen.Generate[rune, runes.Pos](calc(t), cfg{Package: "probe", Name: "Calc"})
	if err != nil {
		t.Fatal(err)
	}

	gentest.Run(t, map[string]string{"calc.go": string(src), "calc_test.go": calcProbe})
}

const calcProbe = `package probe

import (
	"strings"
	"testing"
)

func TestCalc(t *testing.T) {
	p := NewCalcParser()

	tree, err := p.ParseString("12+3-45")
	if err != nil {
		t.Fatal(err)
	}

	// sum, and three numbers inside it
	if len(tree.Nodes) != 4 || tree.Nodes[0].Rule != CalcRuleSum || tree.Nodes[0].Size != 4 || tree.End != 7 {
		t.Fatalf("nodes %+v end %d", tree.Nodes, tree.End)
	}

	var numbers []string
	for c := range tree.Children(0) {
		numbers = append(numbers, tree.Text(c))
	}
	if strings.Join(numbers, ",") != "12,3,45" || tree.Nodes[1].Rule.String() != "number" {
		t.Errorf("numbers = %v", numbers)
	}

	// the top level has the sum
	n := 0
	for range tree.Children(-1) {
		n++
	}
	if n != 1 {
		t.Errorf("%d top level nodes", n)
	}

	for in, want := range map[string]string{
		"":       "syntax error at 1:1: unexpected end of input",
		"12+":    "syntax error at 1:4: unexpected end of input",
		"12+x":   "syntax error at 1:4: unexpected 'x'",
		"12 + 3": "syntax error at 1:3: unexpected ' ', expected digit, \"+\", \"-\" or end of input",
		"1\n2":   "syntax error at 1:2: unexpected '\\n'",
		"\n1":    "syntax error at 1:1: unexpected '\\n', expected digit",
	} {
		_, err := p.ParseString(in)
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%q: %v, want %q", in, err, want)
		}
	}

	// the parser is reused, and a clone outlives the next parse
	first, _ := p.ParseString("1+2")
	kept := first.Clone()
	_, _ = p.ParseString("999")
	if kept.Text(1) != "1" || kept.Text(2) != "2" {
		t.Errorf("clone changed: %q %q", kept.Text(1), kept.Text(2))
	}
}
`

func TestGeneratedParsersForOtherElementTypes(t *testing.T) {
	// bytes: lines of letters, separated by 10
	letter := entity.Between[byte, int]('a', 'z')
	line := repetition.OneOrMore[byte, int](letter).SetPossessive(true)
	lines := rule.New[byte, int]("lines", concatenation.New[byte, int](
		line, repetition.Any[byte, int](concatenation.New[byte, int](entity.Equals[byte, int](10), line)).SetPossessive(true),
	))

	bytesSrc, err := codegen.Generate[byte, int](lines, codegen.Config[byte, int]{Package: "probe", Name: "Lines"})
	if err != nil {
		t.Fatal(err)
	}

	// ints: a number in 0..9 or -9..-1, then a 100
	digits := entity.In[int, int](entity.Range[int]{Lo: -9, Hi: -1}, entity.Range[int]{Lo: 0, Hi: 9})
	ints := rule.New[int, int]("ints", concatenation.New[int, int](
		repetition.OneOrMore[int, int](digits).SetPossessive(true), entity.Equals[int, int](100).Printed("the hundred"),
	))

	intsSrc, err := codegen.Generate[int, int](ints, codegen.Config[int, int]{Package: "probe", Name: "Ints"})
	if err != nil {
		t.Fatal(err)
	}

	// strings: let name = value
	word := entity.NotIn[string, int](entity.Range[string]{Lo: "=", Hi: "="}).Named("a word")
	stmt := rule.New[string, int]("stmt", concatenation.New[string, int](
		vector.Literal[string, int]("let"), word, entity.Equals[string, int]("=").Printed("the ="), word,
	))

	wordsSrc, err := codegen.Generate[string, int](stmt, codegen.Config[string, int]{Package: "probe", Name: "Stmt"})
	if err != nil {
		t.Fatal(err)
	}

	gentest.Run(t, map[string]string{"lines.go": string(bytesSrc), "ints.go": string(intsSrc), "stmt.go": string(wordsSrc), "types_test.go": typesProbe})
}

const typesProbe = `package probe

import "testing"

func TestElementTypes(t *testing.T) {
	// bytes
	lines, err := NewLinesParser().Parse([]byte("ab\ncd\nef"))
	if err != nil || lines.End != 8 || lines.Text(0) != "ab\ncd\nef" {
		t.Fatalf("lines: %v %v", lines, err)
	}

	// a prefix matches: "ab" is a line, and the empty line after it is not another one
	short, err := NewLinesParser().Parse([]byte("ab\n\ncd"))
	if err != nil || short.End != 2 {
		t.Errorf("lines of a document with an empty line: %+v %v", short, err)
	}

	// ints, with negative numbers
	ints, err := NewIntsParser().Parse([]int{3, -4, 0, 100, 7})
	if err != nil || ints.End != 4 {
		t.Fatalf("ints: %v %v", ints, err)
	}

	_, err = NewIntsParser().Parse([]int{3, 4, 50})
	if err == nil || err.Error() != "syntax error at element 2: unexpected 50, expected the hundred" {
		t.Errorf("ints error: %v", err)
	}

	// strings
	stmt, err := NewStmtParser().Parse([]string{"let", "x", "=", "y"})
	if err != nil || stmt.End != 4 {
		t.Fatalf("stmt: %v %v", stmt, err)
	}

	_, err = NewStmtParser().Parse([]string{"let", "x", "y"})
	if err == nil || err.Error() != "syntax error at element 2: unexpected y, expected the =" {
		t.Errorf("stmt error: %v", err)
	}
}
`

// tokens: the elements are structs, and the leaves are functions that the generator can not look into
type token struct {
	Kind int
	Text string
}

func TestGeneratedParserOverTokensWithAClassHook(t *testing.T) {
	kind := func(k int, name string) *entity.Entity[token, int] {
		return entity.New[token, int](func(x token) bool { return x.Kind == k }).Named(name)
	}

	num, plus, minus := kind(0, "number"), kind(1, "plus"), kind(2, "minus")

	sum := rule.New[token, int]("sum", concatenation.New[token, int](
		num,
		repetition.Any[token, int](concatenation.New[token, int](
			alternation.New[token, int](plus, minus).SetOrthogonal(true), num,
		)).SetPossessive(true),
	))

	conf := codegen.Config[token, int]{Package: "probe", Name: "Tok", ElemType: "Token"}

	// without a hook the generator can not know what the functions accept
	if _, err := codegen.Generate[token, int](sum, conf); err == nil || !strings.Contains(err.Error(), "Config.Class") {
		t.Fatalf("err = %v, want a pointer to Config.Class", err)
	}

	conf.Class = func(n *ebnf.Node[token, int]) (string, bool) {
		expr, ok := map[string]string{"number": "x.Kind == KindNumber", "plus": "x.Kind == KindPlus", "minus": "x.Kind == KindMinus"}[n.ID]
		return expr, ok
	}

	src, err := codegen.Generate[token, int](sum, conf)
	if err != nil {
		t.Fatal(err)
	}

	gentest.Run(t, map[string]string{"tok.go": string(src), "token.go": tokenDefinition, "tok_test.go": tokenProbe})
}

const tokenDefinition = `package probe

type Token struct {
	Kind int
	Text string
}

const (
	KindNumber = iota
	KindPlus
	KindMinus
)
`

const tokenProbe = `package probe

import "testing"

func TestTokens(t *testing.T) {
	tree, err := NewTokParser().Parse([]Token{{KindNumber, "1"}, {KindPlus, "+"}, {KindNumber, "2"}, {KindMinus, "-"}, {KindNumber, "3"}})
	if err != nil || tree.End != 5 || tree.Nodes[0].Rule != TokRuleSum {
		t.Fatalf("%v %v", tree, err)
	}

	// the sum stops where it can not go on
	tree, _ = NewTokParser().Parse([]Token{{KindNumber, "1"}, {KindPlus, "+"}, {KindPlus, "+"}})
	if tree == nil || tree.End != 1 {
		t.Errorf("tree = %+v", tree)
	}

	_, err = NewTokParser().Parse([]Token{{KindPlus, "+"}})
	if err == nil || err.Error() != "syntax error at element 0: unexpected {1 +}, expected number" {
		t.Errorf("error: %v", err)
	}
}
`

func TestNamesAreMadeUsableAndUnique(t *testing.T) {
	a := rule.New[rune, runes.Pos]("string literal", some(between('a', 'z')))
	b := rule.New[rune, runes.Pos]("string-literal", some(between('0', '9')))
	c := rule.New[rune, runes.Pos]("9lives", text("x"))

	grammar := conc(a, b, c)

	src, err := codegen.Generate[rune, runes.Pos](grammar, cfg{Package: "probe", Commit: true})
	if err != nil {
		t.Fatal(err)
	}

	gentest.Run(t, map[string]string{"names.go": string(src), "names_test.go": `package probe

import "testing"

func TestNames(t *testing.T) {
	tree, err := NewParser().ParseString("ab12x")
	if err != nil || len(tree.Nodes) != 3 {
		t.Fatalf("%v %v", tree, err)
	}

	got := []string{tree.Nodes[0].Rule.String(), tree.Nodes[1].Rule.String(), tree.Nodes[2].Rule.String()}
	if got[0] != "string literal" || got[1] != "string-literal" || got[2] != "9lives" {
		t.Errorf("rule names %v", got)
	}

	if RuleStringLiteral == RuleStringLiteral2 {
		t.Error("two rules have the same constant")
	}
}
`})
}

var _ = except.New[rune, runes.Pos]

func TestGeneratedCodeForLongLiteralsAtomicRulesAndDeepNesting(t *testing.T) {
	// a literal longer than eight elements is compared in a loop
	long := rule.New[rune, runes.Pos]("long", text("abcdefghijkl"))

	// an atomic rule is reported as a whole, not by the leaves inside it
	letters := some(between('a', 'z').Printed("letter"))
	word := rule.New[rune, runes.Pos]("word", letters).SetAtomic(true)

	// nest = "(" nest ")" | "x"
	nest := rule.Declare[rune, runes.Pos]("nest")
	nest.Define(alt(conc(text("("), nest, text(")")), text("x")).SetOrthogonal(true))

	files := map[string]string{}

	for name, g := range map[string]pat{"Long": long, "Word": word, "Nest": nest} {
		if r, ok := g.(*rule.Rule[rune, runes.Pos]); ok && name != "Nest" {
			if _, err := optimize.Optimize[rune, runes.Pos](r, optimize.Options{Closed: true}); err != nil {
				t.Fatal(err)
			}
		}

		src, err := codegen.Generate[rune, runes.Pos](g, cfg{Package: "probe", Name: name})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		files[strings.ToLower(name)+".go"] = string(src)
	}

	files["edge_test.go"] = `package probe

import (
	"errors"
	"strings"
	"testing"
)

func TestLongLiteral(t *testing.T) {
	p := NewLongParser()

	if tree, err := p.ParseString("abcdefghijkl!"); err != nil || tree.End != 12 {
		t.Fatalf("%v %v", tree, err)
	}

	// wrong at the first, in the middle, at the last element, and too short
	for _, in := range []string{"xbcdefghijkl", "abcdefxhijkl", "abcdefghijkx", "abcdefghijk", ""} {
		if _, err := p.ParseString(in); err == nil {
			t.Errorf("%q matched", in)
		}
	}
}

func TestAtomicRuleIsReportedAsAWhole(t *testing.T) {
	p := NewWordParser()

	if tree, err := p.ParseString("abc1"); err != nil || tree.End != 3 {
		t.Fatalf("%v %v", tree, err)
	}

	_, err := p.ParseString("123")
	if err == nil || err.Error() != "syntax error at 1:1: unexpected '1', expected word" {
		t.Errorf("error: %v", err)
	}
}

func TestNestingLimit(t *testing.T) {
	nested := func(n int) string { return strings.Repeat("(", n) + "x" + strings.Repeat(")", n) }

	p := NewNestParser()

	if tree, err := p.ParseString(nested(40)); err != nil || tree.End != 81 {
		t.Fatalf("%v %v", tree, err)
	}

	p.SetMaxDepth(50)

	if _, err := p.ParseString(nested(40)); err != nil {
		t.Errorf("within the limit: %v", err)
	}

	if _, err := p.ParseString(nested(100)); !errors.Is(err, NestErrTooDeep) {
		t.Errorf("beyond the limit: %v", err)
	}

	// and the parser is usable again after it
	if _, err := p.ParseString(nested(10)); err != nil {
		t.Errorf("after the limit was hit: %v", err)
	}
}
`

	gentest.Run(t, files)
}

func TestFormatAndImportsForElementsOfYourOwnType(t *testing.T) {
	type level int

	// the elements are a named integer type, which the generated code refers to by name
	lo, hi := entity.Between[level, int](1, 3), entity.Equals[level, int](9)
	g := rule.New[level, int]("g", concatenation.New[level, int](
		repetition.OneOrMore[level, int](lo).SetPossessive(true), hi,
	))

	// Format writes every element the grammar mentions as Go code
	src, err := codegen.Generate[level, int](g, codegen.Config[level, int]{
		Package: "probe", Name: "Lv", ElemType: "Level",
		Format: func(v level) string { return fmt.Sprintf("Level(%d)", v) },
	})
	if err != nil {
		t.Fatal(err)
	}

	gentest.Run(t, map[string]string{"lv.go": string(src), "level.go": "package probe\n\ntype Level int\n", "lv_test.go": `package probe

import "testing"

func TestLevels(t *testing.T) {
	tree, err := NewLvParser().Parse([]Level{1, 2, 3, 9})
	if err != nil || tree.End != 4 {
		t.Fatalf("%v %v", tree, err)
	}

	if _, err := NewLvParser().Parse([]Level{1, 4, 9}); err == nil {
		t.Error("4 is not a level")
	}
}
`})
}

func TestClassesThatNeedNoTestForTheElement(t *testing.T) {
	// any element, and an empty class: the generated test does not look at the element, which must still compile
	anyOne := rule.New[rune, runes.Pos]("any", entity.Any[rune, runes.Pos]())
	none := rule.New[rune, runes.Pos]("none", entity.In[rune, runes.Pos]())

	files := map[string]string{}

	for name, g := range map[string]pat{"Any": anyOne, "None": none} {
		for _, utf8 := range []bool{false, true} {
			src, err := codegen.Generate[rune, runes.Pos](g, cfg{Package: "probe", Name: name + map[bool]string{false: "R", true: "U"}[utf8], UTF8: utf8})
			if err != nil {
				t.Fatal(err)
			}

			files[strings.ToLower(name)+map[bool]string{false: "r", true: "u"}[utf8]+".go"] = string(src)
		}
	}

	files["any_test.go"] = `package probe

import "testing"

func TestAny(t *testing.T) {
	if tree, err := NewAnyRParser().ParseString("xy"); err != nil || tree.End != 1 {
		t.Errorf("runes: %v %v", tree, err)
	}

	// a character of several bytes is one element
	if tree, err := NewAnyUParser().Parse("é!"); err != nil || tree.End != 2 {
		t.Errorf("utf8: %v %v", tree, err)
	}

	if _, err := NewAnyRParser().ParseString(""); err == nil {
		t.Error("any element matched nothing")
	}

	if _, err := NewNoneRParser().ParseString("x"); err == nil {
		t.Error("an empty class matched")
	}

	if _, err := NewNoneUParser().Parse("é"); err == nil {
		t.Error("an empty class matched")
	}
}
`

	gentest.Run(t, files)
}

func TestUTF8InputWithMultiByteCharactersAndBrokenBytes(t *testing.T) {
	letter := entity.In[rune, runes.Pos](entity.Range[rune]{Lo: 'a', Hi: 'z'}, entity.Range[rune]{Lo: 'é', Hi: 'é'}, entity.Range[rune]{Lo: 'λ', Hi: 'λ'}).Printed("letter")
	word := rule.New[rune, runes.Pos]("word", some(letter).SetPossessive(true))
	greeting := rule.New[rune, runes.Pos]("greeting", conc(text("héllo"), text(" "), word, maybe(text("🙂")).SetPossessive(true), end.New[rune, runes.Pos]().Printed("end of input")))

	src, err := codegen.Generate[rune, runes.Pos](greeting, cfg{Package: "probe", Name: "Hi", UTF8: true})
	if err != nil {
		t.Fatal(err)
	}

	// a literal with a character that is not valid text is refused
	bad := rule.New[rune, runes.Pos]("bad", vector.Literal[rune, runes.Pos](0xD800))
	if _, err := codegen.Generate[rune, runes.Pos](bad, cfg{Package: "probe", UTF8: true}); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Errorf("err = %v, want a refusal of a literal that is not valid UTF-8", err)
	}

	// text needs rune elements
	if _, err := codegen.Generate[byte, int](rule.New[byte, int]("b", entity.Equals[byte, int](1)), codegen.Config[byte, int]{Package: "probe", UTF8: true}); err == nil || !strings.Contains(err.Error(), "runes") {
		t.Errorf("err = %v, want UTF8 to need runes", err)
	}

	// what the grammar says about inputs that do not match, with characters of several bytes and bytes that are not text:
	// the generated parser has to say the same, with line and column counted in characters
	var cases strings.Builder

	for _, in := range []string{"héllo é1", "héllo \xff", "héllo", "hello", "héllo \xffa", "\xffhéllo", "héllo λ🙂x", "héllo \n", "", "héllo é\xff"} {
		rd, _ := runes.New(strings.NewReader(in))
		log := runes.NewFarthestLog(rd)

		ok, _, err := ebnf.Parse[rune, runes.Pos](greeting, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))
		if err != nil {
			t.Fatal(err)
		}

		want := ""
		if e := log.Err(); !ok && e != nil {
			want = e.Error()
		}

		fmt.Fprintf(&cases, "\t{%q, %v, %q},\n", in, ok, want)
	}

	gentest.Run(t, map[string]string{"hi.go": string(src), "hi_test.go": `package probe

import (
	"testing"
	"unsafe"
)

var cases = []struct {
	in   string
	ok   bool
	want string
}{
` + cases.String() + `}

func TestAgainstTheGrammar(t *testing.T) {
	p := NewHiParser()

	for _, c := range cases {
		tree, err := p.Parse(c.in)

		if (err == nil) != c.ok {
			t.Errorf("%q: matched=%v (%v), the grammar says %v", c.in, err == nil, err, c.ok)
			continue
		}

		if err != nil && c.want != "" && err.Error() != c.want {
			t.Errorf("%q:\n  generated %q\n  grammar   %q", c.in, err.Error(), c.want)
		}

		_ = tree
	}
}

func TestUTF8(t *testing.T) {
	p := NewHiParser()

	tree, err := p.Parse("héllo λé🙂")
	if err != nil {
		t.Fatal(err)
	}

	// positions are bytes: h é(2) l l o space = 7 bytes, the word is λ(2) é(2), the smiley is 4
	if tree.End != 7+2+2+4 {
		t.Errorf("end = %d", tree.End)
	}

	word, _ := tree.FirstWord(0)
	if tree.Text(word) != "λé" {
		t.Errorf("word = %q", tree.Text(word))
	}

	// the text of a node is a slice of the input, not a copy
	if unsafe.StringData(tree.Text(word)) != unsafe.StringData(tree.Input[7:]) {
		t.Error("Text copied the input")
	}

	// the position of a node counts characters
	if line, col := tree.Position(7); line != 1 || col != 7 {
		t.Errorf("position of the word: %d:%d", line, col)
	}

	if tree, err := p.ParseBytes([]byte("héllo ab")); err != nil || tree.End != 9 {
		t.Errorf("bytes: %v %v", tree, err)
	}
}
`})
}

func TestTreeHelpers(t *testing.T) {
	number := rule.New[rune, runes.Pos]("number", some(between('0', '9')).SetPossessive(true))
	op := rule.New[rune, runes.Pos]("operator", alt(text("+"), text("-")).SetOrthogonal(true))
	sum := rule.New[rune, runes.Pos]("sum", conc(number, many(conc(op, number)).SetPossessive(true)))
	doc := rule.New[rune, runes.Pos]("document", conc(sum, text("\n"), sum))

	src, err := codegen.Generate[rune, runes.Pos](doc, cfg{Package: "probe", Name: "T"})
	if err != nil {
		t.Fatal(err)
	}

	gentest.Run(t, map[string]string{"t.go": string(src), "t_test.go": `package probe

import (
	"strings"
	"testing"
)

func TestHelpers(t *testing.T) {
	tree, err := NewTParser().ParseString("12+3\n4-56-7")
	if err != nil {
		t.Fatal(err)
	}

	if tree.Rule(0) != TRuleDocument {
		t.Fatalf("rule = %v", tree.Rule(0))
	}

	// two sums below the document
	var sums []int
	for s := range tree.FindSum(0) {
		sums = append(sums, s)
	}
	if len(sums) != 2 {
		t.Fatalf("%d sums", len(sums))
	}

	// the numbers and operators below the second sum
	var seen []string
	for c := range tree.Children(sums[1]) {
		seen = append(seen, tree.Rule(c).String()+"="+tree.Text(c))
	}
	if strings.Join(seen, " ") != "number=4 operator=- number=56 operator=- number=7" {
		t.Errorf("children: %v", seen)
	}

	if first, ok := tree.FirstNumber(sums[0]); !ok || tree.Text(first) != "12" {
		t.Errorf("first number: %d %v", first, ok)
	}

	if _, ok := tree.FirstOperator(0); ok {
		t.Error("the operators are below the sums, not directly below the document")
	}

	var ops int
	for range tree.Find(sums[0], TRuleOperator) {
		ops++
	}
	if ops != 1 {
		t.Errorf("%d operators", ops)
	}

	// the spans and the positions
	b, e := tree.Span(sums[1])
	if b != 5 || e != 11 {
		t.Errorf("span = %d-%d", b, e)
	}

	if line, col := tree.Position(b); line != 2 || col != 1 {
		t.Errorf("position of the second sum: %d:%d", line, col)
	}
	if line, col := tree.Position(8); line != 2 || col != 4 {
		t.Errorf("position of offset 8: %d:%d", line, col)
	}
	if line, col := tree.Position(0); line != 1 || col != 1 {
		t.Errorf("position 0: %d:%d", line, col)
	}
	if line, col := tree.Position(999); line != 2 || col != 7 {
		t.Errorf("a position past the end is the end: %d:%d", line, col)
	}

	// walking: skip the children of the first sum
	var visited []string
	tree.Walk(func(node, depth int) bool {
		visited = append(visited, strings.Repeat(".", depth)+tree.Rule(node).String())
		return node != sums[0]
	})
	if got := strings.Join(visited, " "); !strings.HasPrefix(got, "document .sum .sum") {
		t.Errorf("visited: %s", got)
	}
	if !strings.Contains(strings.Join(visited, " "), ".sum ..number ..operator ..number ..operator ..number") {
		t.Errorf("the second sum was not entered: %v", visited)
	}
	for _, v := range visited[:3] {
		if strings.Contains(v, "number") {
			t.Errorf("the first sum was entered: %v", visited)
		}
	}

	dump := tree.Dump()
	if !strings.HasPrefix(dump, "document 0-11 \"12+3\\n4-56-7\"\n  sum 0-4 \"12+3\"\n") {
		t.Errorf("dump:\n%s", dump)
	}
}
`})
}

// statements = { "let" name "=" number ";" } end : a language to recover in
func statements(t *testing.T) (program pat, stmt *rule.Rule[rune, runes.Pos]) {
	t.Helper()

	space := entity.OneOf[rune, runes.Pos](' ', '\n', '\t')
	ws := many(space).SetPossessive(true)
	letter := between('a', 'z').Printed("letter")
	digit := between('0', '9').Printed("digit")

	ident := rule.New[rune, runes.Pos]("ident", some(letter))
	number := rule.New[rune, runes.Pos]("number", some(digit))

	stmt = rule.New[rune, runes.Pos]("stmt", conc(text("let"), some(space), ident, ws, text("="), ws, number, ws, text(";"), ws))
	program = rule.New[rune, runes.Pos]("program", conc(ws, many(stmt), end.New[rune, runes.Pos]().Printed("end of input")))

	if res, err := optimize.Optimize[rune, runes.Pos](program, optimize.Options{}); err != nil || len(res.Blocked()) != 0 {
		t.Fatalf("%v\n%v", err, res)
	}

	return program, stmt
}

func TestErrorRecovery(t *testing.T) {
	var files = map[string]string{}

	for _, utf8 := range []bool{false, true} {
		program, _ := statements(t)

		// after a syntax error in a statement, skip to the next ; (and the white space after it) and go on
		resume := conc(text(";"), many(entity.OneOf[rune, runes.Pos](' ', '\n', '\t')).SetPossessive(true))

		name := map[bool]string{false: "R", true: "U"}[utf8]

		src, err := codegen.Generate[rune, runes.Pos](program, cfg{
			Package: "probe", Name: name, UTF8: utf8,
			Recover: map[string]codegen.Recovery[rune, runes.Pos]{"stmt": {Until: resume, Consume: true}},
		})
		if err != nil {
			t.Fatal(err)
		}

		files[strings.ToLower(name)+".go"] = string(src)
	}

	files["recover_test.go"] = `package probe

import (
	"strings"
	"testing"
)

func stmts(tree *RTree) []string {
	var out []string
	for c := range tree.FindStmt(0) {
		out = append(out, strings.TrimSpace(tree.Text(c)))
	}

	return out
}

func TestRecovery(t *testing.T) {
	p := NewRParser()

	// no errors: ParseAll is Parse
	valid := "let a = 1;\nlet b = 2;\n"
	strict, err := p.ParseString(valid)
	if err != nil {
		t.Fatal(err)
	}
	want := strict.Dump()

	tree, errs := p.ParseAll([]rune(valid))
	if len(errs) != 0 || tree == nil || tree.Dump() != want {
		t.Fatalf("a valid document: %v %v", errs, tree)
	}

	// an error in the middle: the statements before and after it are parsed
	in := "let a = 1;\nlet = 2;\nlet b = 3;\n"
	_, strictErr := p.ParseString(in)

	tree, errs = p.ParseAll([]rune(in))
	if tree == nil || len(errs) != 1 {
		t.Fatalf("tree %v errors %v", tree, errs)
	}

	if errs[0].Error() != strictErr.Error() {
		t.Errorf("the first error is the one Parse reports:\n  %v\n  %v", errs[0], strictErr)
	}

	if tree.End != len([]rune(in)) {
		t.Errorf("end = %d", tree.End)
	}

	got := stmts(tree)
	if len(got) != 3 || got[0] != "let a = 1;" || got[1] != "let = 2;" || got[2] != "let b = 3;" {
		t.Errorf("statements: %q", got)
	}

	// the skipped text is an error node inside the statement
	second := -1
	n := 0
	for c := range tree.FindStmt(0) {
		if n++; n == 2 {
			second = c
		}
	}
	e, ok := tree.First(second, RRuleError)
	if !ok || strings.TrimSpace(tree.Text(e)) != "let = 2;" {
		t.Errorf("error node: %d %v", e, ok)
	}

	// several errors, and one at the end of the input
	tree, errs = p.ParseAll([]rune("let = 1; let b = 2; let c = ; let d = 4; let e ="))
	if tree == nil || len(errs) != 3 {
		t.Fatalf("tree %v errors %v", tree, errs)
	}
	if got := stmts(tree); len(got) != 5 {
		t.Errorf("statements: %q", got)
	}
	if !strings.Contains(errs[2].Error(), "unexpected end of input") {
		t.Errorf("last error: %v", errs[2])
	}

	// a statement that was recovered has the error node and nothing of what matched before the error: "let c = ;" had
	// an ident, which is not part of the tree
	for c := range tree.FindStmt(0) {
		if _, recovered := tree.First(c, RRuleError); !recovered {
			continue
		}

		n := 0
		for range tree.Children(c) {
			n++
		}

		if n != 1 {
			t.Errorf("the recovered statement %q has %d children, want only the error", tree.Text(c), n)
		}
	}

	// garbage before a statement is not an error in it, and nothing else takes it: the parse fails
	tree, errs = p.ParseAll([]rune("let a = 1; ??? let b = 2;"))
	if tree != nil || len(errs) != 1 || !strings.Contains(errs[0].Error(), "unexpected '?'") {
		t.Errorf("tree %v errors %v", tree, errs)
	}

	// the parser is in a good state afterwards, and Parse does not recover
	if _, err := p.ParseString(in); err == nil {
		t.Error("Parse recovered")
	}
	if _, err := p.ParseString(valid); err != nil {
		t.Error(err)
	}
}

func TestRecoveryInText(t *testing.T) {
	p := NewUParser()

	in := "let é = 1;\nlet b = 2;\nlet ü = λ; let c = 3;"
	tree, errs := p.ParseAll(in)
	if tree == nil || len(errs) != 2 {
		t.Fatalf("tree %v errors %v", tree, errs)
	}

	// the lines and columns count characters, and the skipping moved by characters
	if errs[0].Error() != "syntax error at 1:5: unexpected 'é', expected letter" {
		t.Errorf("first: %v", errs[0])
	}
	if !strings.HasPrefix(errs[1].Error(), "syntax error at 3:5: unexpected 'ü'") {
		t.Errorf("second: %v", errs[1])
	}

	var names []string
	for c := range tree.FindStmt(0) {
		names = append(names, strings.TrimSpace(tree.Text(c)))
	}
	if len(names) != 4 || names[3] != "let c = 3;" {
		t.Errorf("statements: %q", names)
	}

	if tree.End != len(in) {
		t.Errorf("end = %d, want %d", tree.End, len(in))
	}
}
`

	gentest.Run(t, files)
}

func TestRecoveryIsCheckedWhenGenerating(t *testing.T) {
	program, _ := statements(t)

	for name, recover := range map[string]map[string]codegen.Recovery[rune, runes.Pos]{
		"no such rule":      {"nothing": {Until: text(";")}},
		"no Until":          {"stmt": {}},
		"Until uses a rule": {"stmt": {Until: rule.New[rune, runes.Pos]("r", text(";"))}},
		"Until backtracks":  {"stmt": {Until: alt(text("a"), text("ab"))}},
	} {
		_, err := codegen.Generate[rune, runes.Pos](program, cfg{Package: "p", Recover: recover})
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestLeftRecursionOverOtherElementTypes(t *testing.T) {
	// bytes: the analysis maps them to integers, so the loop can be proven
	digit := entity.Between[byte, int]('0', '9')
	num := rule.New[byte, int]("num", repetition.OneOrMore[byte, int](digit).SetPossessive(true))
	sum := rule.Declare[byte, int]("sum").SetLeftRecursive(true)
	sum.Define(alternation.New[byte, int](
		concatenation.New[byte, int](sum, entity.Equals[byte, int]('+'), num), num,
	))

	src, err := codegen.Generate[byte, int](sum, codegen.Config[byte, int]{Package: "probe", Name: "Sum"})
	if err != nil {
		t.Fatal(err)
	}

	gentest.Run(t, map[string]string{"sum.go": string(src), "sum_test.go": `package probe

import "testing"

func TestSum(t *testing.T) {
	tree, err := NewSumParser().Parse([]byte("1+22+333"))
	if err != nil || tree.End != 8 {
		t.Fatalf("%v %v", tree, err)
	}

	// one node for the chain, and the three numbers and nothing for the plus signs
	n := 0
	for range tree.FindNum(0) {
		n++
	}
	if len(tree.Nodes) != 4 || n != 3 {
		t.Errorf("%d nodes, %d numbers", len(tree.Nodes), n)
	}
}
`})

	// tokens: nothing is known about what the functions accept. Alone, with one base and one tail and nothing after it, the
	// rule is a loop whatever the elements are; followed by something, the tail might be what follows, which can not be told
	kind := func(k int, name string) *entity.Entity[token, int] {
		return entity.New[token, int](func(x token) bool { return x.Kind == k }).Named(name)
	}

	tsum := rule.Declare[token, int]("sum").SetLeftRecursive(true)
	tsum.Define(alternation.New[token, int](
		concatenation.New[token, int](tsum, kind(1, "plus"), kind(0, "number")), kind(0, "number"),
	))

	followed := concatenation.New[token, int](tsum, kind(2, "semicolon"))

	conf := codegen.Config[token, int]{
		Package: "probe", ElemType: "Token",
		Class: func(n *ebnf.Node[token, int]) (string, bool) {
			return map[string]string{"plus": "x.Kind == KindPlus", "number": "x.Kind == KindNumber"}[n.ID], true
		},
	}

	conf.Class = func(n *ebnf.Node[token, int]) (string, bool) {
		return map[string]string{"plus": "x.Kind == KindPlus", "number": "x.Kind == KindNumber", "semicolon": "x.Kind == KindSemicolon"}[n.ID], true
	}

	if _, err := codegen.Generate[token, int](tsum, conf); err != nil {
		t.Errorf("alone: %v", err)
	}

	_, err = codegen.Generate[token, int](followed, conf)
	if got := strings.Join(problems(t, err), "\n"); !strings.Contains(got, "can not be shown to match like a loop") {
		t.Errorf("followed: problems:\n%s", got)
	}

	conf.Commit = true
	if _, err := codegen.Generate[token, int](followed, conf); err != nil {
		t.Errorf("Commit: %v", err)
	}
}

// typedGrammar is a small language that has every kind of child: one, one or none, many, and a choice.
//
//	doc   = pair { ";" pair }
//	pair  = name ":" value [ "!" flag ]
//	value = number | text | list
//	list  = "[" [ value { "," value } ] "]"
//
// The rule text is there for its name: Text is a method every view has.
func typedGrammar() pat {
	number := rule.New[rune, runes.Pos]("number", some(between('0', '9')).SetPossessive(true))
	name := rule.New[rune, runes.Pos]("name", some(between('a', 'z')).SetPossessive(true))
	txt := rule.New[rune, runes.Pos]("text", some(between('A', 'Z')).SetPossessive(true))
	flag := rule.New[rune, runes.Pos]("flag", text("yes"))
	value := rule.Declare[rune, runes.Pos]("value")
	list := rule.New[rune, runes.Pos]("list", conc(text("["), maybe(conc(value, many(conc(text(","), value)).SetPossessive(true))).SetPossessive(true), text("]")))
	value.Define(alt(number, txt, list).SetOrthogonal(true))
	pair := rule.New[rune, runes.Pos]("pair", conc(name, text(":"), value, maybe(conc(text("!"), flag)).SetPossessive(true)))

	return rule.New[rune, runes.Pos]("doc", conc(pair, many(conc(text(";"), pair)).SetPossessive(true)))
}

func TestTypedASTViewsFollowTheGrammar(t *testing.T) {
	src, err := codegen.Generate[rune, runes.Pos](typedGrammar(), cfg{Package: "probe", Name: "V", AST: true})
	if err != nil {
		t.Fatal(err)
	}

	gentest.Run(t, map[string]string{"v.go": string(src), "v_test.go": `package probe

import (
	"iter"
	"slices"
	"testing"
)

// the types of the methods are what the grammar promises
var (
	_ func() iter.Seq[VPair]  = VDoc{}.Pairs
	_ func() VName            = VPair{}.Name
	_ func() VValue           = VPair{}.Value
	_ func() (VFlag, bool)    = VPair{}.Flag
	_ func() iter.Seq[VValue] = VList{}.Values
	_ func() (VNumber, bool)  = VValue{}.Number
	_ func() (VText, bool)    = VValue{}.TextRule // Text is taken by the text of the node
	_ func() (VList, bool)    = VValue{}.List
	_ func() (VRule, bool)    = VValue{}.Kind
	_ func() VDoc             = VTop{}.Doc
)

func TestViews(t *testing.T) {
	const in = "a:12;bc:[X,[],7]!yes;d:Y"

	tree, err := NewVParser().ParseString(in)
	if err != nil {
		t.Fatal(err)
	}

	doc := tree.Top().Doc()
	if got := doc.Text(); got != in {
		t.Fatalf("doc = %q", got)
	}

	pairs := slices.Collect(doc.Pairs())
	if len(pairs) != 3 {
		t.Fatalf("%d pairs", len(pairs))
	}

	var names []string
	for _, p := range pairs {
		names = append(names, p.Name().Text())
	}
	if !slices.Equal(names, []string{"a", "bc", "d"}) {
		t.Errorf("names = %v", names)
	}

	// a choice: which one it is, and the view of it
	first := pairs[0].Value()
	if k, ok := first.Kind(); !ok || k != VRuleNumber {
		t.Errorf("kind = %v %v", k, ok)
	}
	if n, ok := first.Number(); !ok || n.Text() != "12" {
		t.Errorf("number = %v %v", n, ok)
	}
	if _, ok := first.List(); ok {
		t.Error("12 is not a list")
	}

	// one or none
	if _, ok := pairs[0].Flag(); ok {
		t.Error("the first pair has no flag")
	}
	if f, ok := pairs[1].Flag(); !ok || f.Text() != "yes" {
		t.Errorf("flag = %v %v", f, ok)
	}

	// many, nested
	list, ok := pairs[1].Value().List()
	if !ok {
		t.Fatal("not a list")
	}
	var kinds []VRule
	for v := range list.Values() {
		k, _ := v.Kind()
		kinds = append(kinds, k)
	}
	if !slices.Equal(kinds, []VRule{VRuleText, VRuleList, VRuleNumber}) {
		t.Errorf("kinds = %v", kinds)
	}

	// stopping early
	n := 0
	for range list.Values() {
		n++
		break
	}
	if n != 1 {
		t.Errorf("break: %d", n)
	}

	// the span, and the way back to the tree
	if b, e := pairs[2].Span(); b != 21 || e != 24 {
		t.Errorf("span = %d-%d", b, e)
	}
	if v, ok := tree.AsValue(pairs[2].Value().Node); !ok || v.Text() != "Y" {
		t.Errorf("AsValue = %v %v", v, ok)
	}
	if _, ok := tree.AsPair(pairs[2].Value().Node); ok {
		t.Error("a value is not a pair")
	}
	if _, ok := tree.AsPair(-1); ok {
		t.Error("-1 is not a node")
	}
	if _, ok := tree.AsPair(len(tree.Nodes)); ok {
		t.Error("past the nodes is not a node")
	}

	// an empty list has no values
	empty, _ := slices.Collect(list.Values())[1].List()
	if len(slices.Collect(empty.Values())) != 0 {
		t.Error("[] has values")
	}
}
`})
}

func TestTypedASTWithRecovery(t *testing.T) {
	program, _ := statements(t)

	resume := conc(text(";"), many(entity.OneOf[rune, runes.Pos](' ', '\n', '\t')).SetPossessive(true))

	files := map[string]string{}

	for _, utf8 := range []bool{false, true} {
		name := map[bool]string{false: "R", true: "U"}[utf8]

		src, err := codegen.Generate[rune, runes.Pos](program, cfg{
			Package: "probe", Name: name, UTF8: utf8, AST: true,
			Recover: map[string]codegen.Recovery[rune, runes.Pos]{"stmt": {Until: resume, Consume: true}},
		})
		if err != nil {
			t.Fatal(err)
		}

		files[strings.ToLower(name)+".go"] = string(src)
	}

	files["v_test.go"] = `package probe

import (
	"iter"
	"slices"
	"testing"
)

// a statement that can be cut short does not promise its ident and number
var (
	_ func() (RIdent, bool)  = RStmt{}.Ident
	_ func() (RNumber, bool) = RStmt{}.Number
	_ func() bool            = RStmt{}.Recovered
	_ func() iter.Seq[RStmt] = RProgram{}.Stmts
	_ func() (UIdent, bool)  = UStmt{}.Ident
)

func TestRecoveredStatementsAreViewedAsCutShort(t *testing.T) {
	tree, errs := NewRParser().ParseAll([]rune("let a = 1;\nlet b = ;\nlet c = 3;\n"))
	if len(errs) != 1 {
		t.Fatalf("errors: %v", errs)
	}

	var idents []string
	var recovered []bool
	var numbers []bool

	for s := range tree.Top().Program().Stmts() {
		recovered = append(recovered, s.Recovered())

		_, hasNumber := s.Number()
		numbers = append(numbers, hasNumber)

		if id, ok := s.Ident(); ok {
			idents = append(idents, id.Text())
		}
	}

	// the middle statement was cut short and has nothing but the error: not even the ident that was read before it
	if !slices.Equal(idents, []string{"a", "c"}) || !slices.Equal(recovered, []bool{false, true, false}) || !slices.Equal(numbers, []bool{true, false, true}) {
		t.Errorf("idents %v recovered %v numbers %v", idents, recovered, numbers)
	}
}

func TestUTF8Views(t *testing.T) {
	tree, err := NewUParser().Parse("let é = 1;")
	if err == nil {
		t.Fatalf("é is not a letter, but parsed: %s", tree.Dump())
	}

	tree, err = NewUParser().Parse("let a = 12; ")
	if err != nil {
		t.Fatal(err)
	}

	s := slices.Collect(tree.Top().Program().Stmts())[0]

	// the slice of the input is a string
	var in string = s.Slice()
	if in != "let a = 12; " {
		t.Errorf("slice = %q", in)
	}
}
`

	gentest.Run(t, files)
}

func TestTypedASTOfLoopsAndNamesThatClash(t *testing.T) {
	number := rule.New[rune, runes.Pos]("number", some(between('0', '9')).SetPossessive(true))
	addop := rule.New[rune, runes.Pos]("addop", alt(text("+"), text("-")).SetOrthogonal(true))
	expr := rule.Declare[rune, runes.Pos]("expr").SetLeftRecursive(true)
	expr.Define(alt(conc(expr, addop, number), number).SetOrthogonal(true))

	// rules that are called like the types of the generated file
	parser := rule.New[rune, runes.Pos]("parser", conc(text("("), expr, text(")")))
	tree := rule.New[rune, runes.Pos]("tree", alt(parser, number).SetOrthogonal(true))
	parse := rule.New[rune, runes.Pos]("parse", conc(tree, many(conc(text(","), tree)).SetPossessive(true)))
	top := rule.New[rune, runes.Pos]("top", parse)
	node := rule.New[rune, runes.Pos]("node", conc(text("<"), top, text(">")))

	res, err := optimize.Analyze[rune, runes.Pos](node, optimize.Options{Closed: true})
	if err != nil {
		t.Fatal(err)
	}

	res.Force()

	// the rules are named like the types, and Name is empty
	src, err := codegen.Generate[rune, runes.Pos](node, cfg{Package: "probe", AST: true})
	if err != nil {
		t.Fatal(err)
	}

	gentest.Run(t, map[string]string{"p.go": string(src), "p_test.go": `package probe

import (
	"iter"
	"slices"
	"testing"
)

var (
	// the loop has all its terms and operators, in the order of their kind
	_ func() iter.Seq[Number] = Expr{}.Numbers
	_ func() iter.Seq[Addop]  = Expr{}.Addops
	// the rules took other names than the types of the file
	_ Parser2 = Parser2{}
	_ Tree2   = Tree2{}
	_ Parse2  = Parse2{}
	_ Node2   = Node2{}
	_ Top2    = Top2{}
	_ *Parser = NewParser()
)

func TestLoopView(t *testing.T) {
	tree, err := NewParser().ParseString("<(1+22-3),4>")
	if err != nil {
		t.Fatal(err)
	}

	n, _ := tree.AsNode2(0)
	p := n.Top().Parse().Trees()
	trees := slices.Collect(p)
	if len(trees) != 2 {
		t.Fatalf("%d trees", len(trees))
	}

	pr, ok := trees[0].Parser()
	if !ok {
		t.Fatal("the first tree is a parser")
	}
	e := pr.Expr()
	var nums, ops []string
	for x := range e.Numbers() {
		nums = append(nums, x.Text())
	}
	for x := range e.Addops() {
		ops = append(ops, x.Text())
	}
	if !slices.Equal(nums, []string{"1", "22", "3"}) || !slices.Equal(ops, []string{"+", "-"}) {
		t.Errorf("numbers %v operators %v", nums, ops)
	}

	if k, ok := trees[1].Kind(); !ok || k != RuleNumber {
		t.Errorf("kind = %v %v", k, ok)
	}
}
`})
}

func TestTypedASTIgnoresWhatAnExceptionOnlyLooksAt(t *testing.T) {
	word := rule.New[rune, runes.Pos]("word", some(between('a', 'z')).SetPossessive(true))
	keyword := rule.New[rune, runes.Pos]("keyword", text("let"))
	ident := rule.New[rune, runes.Pos]("ident", except.New[rune, runes.Pos](word, keyword))

	src, err := codegen.Generate[rune, runes.Pos](ident, cfg{Package: "probe", Name: "E", AST: true, Commit: true})
	if err != nil {
		t.Fatal(err)
	}

	gentest.Run(t, map[string]string{"e.go": string(src), "e_test.go": `package probe

import (
	"reflect"
	"testing"
)

func TestExceptionHasNoView(t *testing.T) {
	tree, err := NewEParser().ParseString("lex")
	if err != nil {
		t.Fatal(err)
	}

	id := tree.Top().Ident()
	if id.Word().Text() != "lex" {
		t.Errorf("word = %q", id.Word().Text())
	}

	// the keyword is only looked at to be refused, it is never in the tree
	if _, has := reflect.TypeOf(id).MethodByName("Keyword"); has {
		t.Error("an ident has no keyword inside")
	}

	if _, err := NewEParser().ParseString("let"); err == nil {
		t.Error("let is a keyword")
	}
}
`})
}

func TestSkippingGarbageBetweenStatements(t *testing.T) {
	program, stmt := statements(t)

	space := entity.OneOf[rune, runes.Pos](' ', '\n', '\t')
	ws := many(space).SetPossessive(true)

	// block = "{" { stmt } "}" end
	block := rule.New[rune, runes.Pos]("block", conc(text("{"), ws, many(stmt).SetPossessive(true), text("}"), ws, end.New[rune, runes.Pos]().Printed("end of input")))

	skip := map[string]codegen.Skip[rune, runes.Pos]{"stmt": {}}

	gen := func(root pat, name string, utf8 bool, c cfg) string {
		c.Package, c.Name, c.UTF8 = "probe", name, utf8

		src, err := codegen.Generate[rune, runes.Pos](root, c)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		return string(src)
	}

	resume := conc(text(";"), many(space).SetPossessive(true))

	files := map[string]string{
		"s.go":  gen(program, "S", false, cfg{Skip: skip}),
		"su.go": gen(program, "SU", true, cfg{Skip: skip}),
		"c.go": gen(program, "C", false, cfg{Skip: map[string]codegen.Skip[rune, runes.Pos]{"stmt": {Until: text("let")}}, Recover: map[string]codegen.Recovery[rune, runes.Pos]{
			"stmt": {Until: resume, Consume: true}}}),
		"t.go": gen(program, "T", false, cfg{Skip: map[string]codegen.Skip[rune, runes.Pos]{"stmt": {Until: text("let")}}}),
		"b.go": gen(block, "B", false, cfg{Skip: map[string]codegen.Skip[rune, runes.Pos]{"stmt": {Stop: text("}")}}}),
		"n.go": gen(block, "N", false, cfg{Skip: skip}),
	}

	// what the grammar says about the first thing that is wrong in a text
	first := func(root pat, in string) string {
		rd, _ := runes.New(strings.NewReader(in))
		log := runes.NewFarthestLog(rd)

		ok, _, err := ebnf.Parse[rune, runes.Pos](root, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))
		if err != nil {
			t.Fatal(err)
		}

		if ok || log.Err() == nil {
			return ""
		}

		return log.Err().Error()
	}

	type tc struct {
		in      string
		errs    int
		stmts   string // the statements, trimmed, with | between
		garbage string // the error nodes
	}

	cases := []tc{
		{"let a = 1;\n@@ junk\nlet b = 2;\n", 1, "let a = 1;|let b = 2;", "@@ junk"},
		{"@@ let a = 1; let b = 2;", 1, "let a = 1;|let b = 2;", "@@"},
		{"let a = 1; ## let b = 2; $$", 2, "let a = 1;|let b = 2;", "##|$$"},
		{"xlet a = 1;", 1, "let a = 1;", "x"},
		{"let a = 1; é€😀 let b = 2;", 1, "let a = 1;|let b = 2;", "é€😀"},
		{"let a = 1;\n@@\n@@@\nlet b = 2;", 1, "let a = 1;|let b = 2;", "@@\n@@@"},
		{"let a = 1; let b = 2;", 0, "let a = 1;|let b = 2;", ""},
		{"", 0, "", ""},
		{"@@", 1, "", "@@"},
	}

	var table strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&table, "\t{%q, %d, %q, %q, %q},\n", c.in, c.errs, c.stmts, c.garbage, first(program, c.in))
	}

	files["skip_test.go"] = `package probe

import (
	"math/rand"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

var cases = []struct {
	in      string
	errs    int
	stmts   string
	garbage string
	message string // the first error, as the grammar says it
}{
` + table.String() + `}

// the texts of the nodes of a rule at the top, and of the errors below the program
func texts(nodes func(yield func(int) bool), text func(int) string) string {
	var out []string
	for n := range nodes {
		out = append(out, strings.TrimSpace(text(n)))
	}

	return strings.Join(out, "|")
}

func TestSkipsTheGarbage(t *testing.T) {
	for _, c := range cases {
		{
			tree, errs := NewSParser().ParseAll([]rune(c.in))
			if tree == nil || len(errs) != c.errs {
				t.Errorf("%q: tree %v errors %v, want %d errors", c.in, tree != nil, errs, c.errs)
				continue
			}

			if tree.End != len(c.in) && len([]rune(c.in)) != tree.End {
				t.Errorf("%q: ends at %d", c.in, tree.End)
			}

			if c.errs > 0 && errs[0].Error() != c.message {
				t.Errorf("%q:\n  error   %q\n  grammar %q", c.in, errs[0], c.message)
			}

			if got := texts(tree.FindStmt(0), tree.Text); got != c.stmts {
				t.Errorf("%q: statements %q, want %q", c.in, got, c.stmts)
			}

			if got := texts(tree.Find(0, SRuleError), tree.Text); got != c.garbage {
				t.Errorf("%q: garbage %q, want %q", c.in, got, c.garbage)
			}
		}

		// the same in UTF-8: the same nodes, and the errors count characters
		{
			tree, errs := NewSUParser().ParseAll(c.in)
			if tree == nil || len(errs) != c.errs {
				t.Errorf("UTF-8 %q: tree %v errors %v", c.in, tree != nil, errs)
				continue
			}

			if tree.End != len(c.in) {
				t.Errorf("UTF-8 %q: ends at %d of %d", c.in, tree.End, len(c.in))
			}

			if c.errs > 0 && errs[0].Error() != c.message {
				t.Errorf("UTF-8 %q:\n  error   %q\n  grammar %q", c.in, errs[0], c.message)
			}

			if got := texts(tree.FindStmt(0), tree.Text); got != c.stmts {
				t.Errorf("UTF-8 %q: statements %q, want %q", c.in, got, c.stmts)
			}

			if got := texts(tree.Find(0, SURuleError), tree.Text); got != c.garbage {
				t.Errorf("UTF-8 %q: garbage %q, want %q", c.in, got, c.garbage)
			}
		}

		// Parse does not skip anything
		if c.errs > 0 {
			if _, err := NewSParser().ParseString(c.in); err == nil || err.Error() != c.message {
				t.Errorf("Parse of %q: %v", c.in, err)
			}
		}
	}
}

// model is what skipping garbage means, said without the parser: at a place where a statement matches it is taken, and where
// none does the text up to the next place where one does (or the end) is garbage.
func model(in string) (kinds, texts []string) {
	stmt := regexp.MustCompile("^let[ \\n\\t]+[a-z]+[ \\n\\t]*=[ \\n\\t]*[0-9]+[ \\n\\t]*;[ \\n\\t]*")
	lead := regexp.MustCompile("^[ \\n\\t]*")

	pos := len(lead.FindString(in))

	for pos < len(in) {
		if m := stmt.FindString(in[pos:]); m != "" {
			kinds, texts = append(kinds, "stmt"), append(texts, m)
			pos += len(m)

			continue
		}

		end := pos
		for end < len(in) {
			_, w := utf8.DecodeRuneInString(in[end:])
			end += w

			if end >= len(in) || stmt.MatchString(in[end:]) {
				break
			}
		}

		kinds, texts = append(kinds, "error"), append(texts, in[pos:end])
		pos = end
	}

	return kinds, texts
}

func TestAgainstTheModel(t *testing.T) {
	pieces := []string{"let a = 1;", "let b = 22; ", " ", "\n", "@@", "x", "é", "😀", "let", "let =", ";", "let c = ;", "=", "1", "let d = 3;\n"}
	rng := rand.New(rand.NewSource(99))

	for range 4000 {
		var sb strings.Builder
		for range rng.Intn(7) {
			sb.WriteString(pieces[rng.Intn(len(pieces))])
		}

		in := sb.String()
		kinds, texts := model(in)

		tree, errs := NewSUParser().ParseAll(in)
		if tree == nil || tree.End != len(in) {
			t.Fatalf("%q: no tree, or it does not reach the end: %v", in, errs)
		}

		var gotKinds, gotTexts []string
		for c := range tree.Children(0) {
			gotKinds, gotTexts = append(gotKinds, tree.Rule(c).String()), append(gotTexts, tree.Text(c))
		}

		if !slices.Equal(gotKinds, kinds) || !slices.Equal(gotTexts, texts) {
			t.Fatalf("%q:\n  parser %q %q\n  model  %q %q", in, gotKinds, gotTexts, kinds, texts)
		}

		if want := strings.Count(strings.Join(kinds, " "), "error"); len(errs) != want {
			t.Fatalf("%q: %d errors, want %d: %v", in, len(errs), want, errs)
		}

		// the runes parser finds the same
		rtree, rerrs := NewSParser().ParseAll([]rune(in))
		if rtree == nil || len(rerrs) != len(errs) || rtree.Dump() == "" {
			t.Fatalf("%q: runes: %v %v", in, rtree != nil, rerrs)
		}

		for i := range errs {
			if errs[i].Error() != rerrs[i].Error() {
				t.Fatalf("%q: error %d differs\n  %q\n  %q", in, i, errs[i], rerrs[i])
			}
		}
	}
}

func TestSkippingAndRecoveringTogether(t *testing.T) {
	// garbage between statements is skipped up to the next let, and a statement that goes wrong in the middle is recovered;
	// with the default the broken statement would have been taken for garbage, because only one that matches ends the skipping
	tree, errs := NewCParser().ParseAll([]rune("let a = 1; ?? let b = ; let c = 3;"))
	if tree == nil || len(errs) != 2 {
		t.Fatalf("tree %v errors %v", tree != nil, errs)
	}

	if got := texts(tree.FindStmt(0), tree.Text); got != "let a = 1;|let b = ;|let c = 3;" {
		t.Errorf("statements %q", got)
	}

	if got := texts(tree.Find(0, CRuleError), tree.Text); got != "??" {
		t.Errorf("garbage at the top %q", got)
	}
}

func TestSkipUntilWhatTheGrammarSays(t *testing.T) {
	const in = "@@ let = 1; let b = 2;"

	// by default: skip until a statement matches, which skips the first let
	tree, errs := NewSParser().ParseAll([]rune(in))
	if tree == nil || len(errs) != 1 {
		t.Fatalf("default: %v %v", tree != nil, errs)
	}

	// until "let": the first let is where the parse goes on, and a statement can not start with it
	ttree, terrs := NewTParser().ParseAll([]rune(in))
	if ttree == nil || len(terrs) != 2 {
		t.Fatalf("until let: %v %v", ttree != nil, terrs)
	}

	if got := texts(ttree.Find(0, TRuleError), ttree.Text); got != "@@|let = 1;" {
		t.Errorf("garbage %q", got)
	}
}

func TestTheRepetitionEndsWhereStopSays(t *testing.T) {
	tree, errs := NewBParser().ParseAll([]rune("{ let a = 1; ?? let b = 2; }"))
	if tree == nil || len(errs) != 1 {
		t.Fatalf("tree %v errors %v", tree != nil, errs)
	}

	if got := texts(tree.FindStmt(0), tree.Text); got != "let a = 1;|let b = 2;" {
		t.Errorf("statements %q", got)
	}

	// and the end of the block is not garbage
	if tree, errs := NewBParser().ParseAll([]rune("{ let a = 1; }")); tree == nil || len(errs) != 0 {
		t.Errorf("a block without garbage: %v %v", tree != nil, errs)
	}

	// without Stop the "}" that closes the block is skipped like the rest, and the block never closes
	ntree, nerrs := NewNParser().ParseAll([]rune("{ let a = 1; }"))
	if ntree != nil || len(nerrs) != 2 {
		t.Errorf("no Stop: tree %v errors %v", ntree != nil, nerrs)
	}
}
`

	gentest.Run(t, files)
}

func positionGrammar() pat {
	num := rule.New[rune, runes.Pos]("num", some(between('0', '9')).SetPossessive(true))
	word := rule.New[rune, runes.Pos]("word", some(between('a', 'z')).SetPossessive(true))
	value := rule.New[rune, runes.Pos]("value", alt(num, word).SetOrthogonal(true))
	pair := rule.New[rune, runes.Pos]("pair", conc(value, text(":"), value))
	span := rule.New[rune, runes.Pos]("span", conc(num, maybe(conc(text(".."), num)).SetPossessive(true)))

	return rule.New[rune, runes.Pos]("doc", conc(pair, text(" "), span))
}

func TestPositionsNameTheChildrenByWhereTheyAre(t *testing.T) {
	src, err := codegen.Generate[rune, runes.Pos](positionGrammar(), cfg{Package: "probe", Name: "P", AST: true,
		Positions: map[string]map[string][]string{
			"pair": {"value": {"Key", "Val"}},
			"span": {"num": {"From", "To"}},
		}})
	if err != nil {
		t.Fatal(err)
	}

	gentest.Run(t, map[string]string{"p.go": string(src), "p_test.go": `package probe

import (
	"iter"
	"testing"
)

var (
	_ func() PValue            = PPair{}.Key // there are always two values in a pair
	_ func() PValue            = PPair{}.Val
	_ func() iter.Seq[PValue]  = PPair{}.Values // and the other way to get them is still there
	_ func() PNum              = PSpan{}.From
	_ func() (PNum, bool)      = PSpan{}.To // the second number may not be there
	_ func() iter.Seq[PNum]    = PSpan{}.Nums
)

func TestNames(t *testing.T) {
	tree, err := NewPParser().ParseString("ab:12 3..45")
	if err != nil {
		t.Fatal(err)
	}

	doc := tree.Top().Doc()
	pair := doc.Pair()
	if pair.Key().Text() != "ab" || pair.Val().Text() != "12" {
		t.Errorf("pair = %q %q", pair.Key().Text(), pair.Val().Text())
	}

	span := doc.SpanRule() // Span is the span of the node
	to, ok := span.To()
	if span.From().Text() != "3" || !ok || to.Text() != "45" {
		t.Errorf("span = %q %q %v", span.From().Text(), to.Text(), ok)
	}

	tree, err = NewPParser().ParseString("a:b 7")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := tree.Top().Doc().SpanRule().To(); ok {
		t.Error("7 has no end")
	}
}
`})
}

func TestPositionsAreCheckedWhenGenerating(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  cfg
		want string
	}{
		"no AST":            {cfg{Positions: map[string]map[string][]string{"pair": {"value": {"A", "B"}}}}, "needs Config.AST"},
		"unknown rule":      {cfg{AST: true, Positions: map[string]map[string][]string{"nope": {"value": {"A"}}}}, "does not have: nope"},
		"unknown child":     {cfg{AST: true, Positions: map[string]map[string][]string{"pair": {"nope": {"A"}}}}, "does not have nope inside"},
		"too many names":    {cfg{AST: true, Positions: map[string]map[string][]string{"pair": {"value": {"A", "B", "C"}}}}, "at most 2 times, and 3 names"},
		"not exported":      {cfg{AST: true, Positions: map[string]map[string][]string{"pair": {"value": {"a", "B"}}}}, "not an exported identifier"},
		"not an identifier": {cfg{AST: true, Positions: map[string]map[string][]string{"pair": {"value": {"A B"}}}}, "not an exported identifier"},
		"every view has it": {cfg{AST: true, Positions: map[string]map[string][]string{"pair": {"value": {"Text"}}}}, "method every view has"},
		"used twice":        {cfg{AST: true, Positions: map[string]map[string][]string{"pair": {"value": {"A", "A"}}}}, "used twice"},
	} {
		tc.cfg.Package = "probe"

		_, err := codegen.Generate[rune, runes.Pos](positionGrammar(), tc.cfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}

	// the same for what Skip is given
	root := positionGrammar()

	for name, tc := range map[string]struct {
		skip map[string]codegen.Skip[rune, runes.Pos]
		want string
	}{
		"unknown rule": {map[string]codegen.Skip[rune, runes.Pos]{"nope": {}}, "does not have: nope"},
		"uses a rule":  {map[string]codegen.Skip[rune, runes.Pos]{"pair": {Stop: rule.New[rune, runes.Pos]("x", text("x"))}}, "uses rules"},
	} {
		_, err := codegen.Generate[rune, runes.Pos](root, cfg{Package: "probe", Skip: tc.skip})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("skip %s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestSkippingStopsWhereItShould(t *testing.T) {
	_, stmt := statements(t)

	space := entity.OneOf[rune, runes.Pos](' ', '\n', '\t')
	ws := many(space).SetPossessive(true)
	closing := end.New[rune, runes.Pos]().Printed("end of input")

	stop := map[string]codegen.Skip[rune, runes.Pos]{"stmt": {Stop: text("}")}}

	block := rule.New[rune, runes.Pos]("block", conc(text("{"), ws, many(stmt).SetPossessive(true), text("}"), ws, closing))
	atomic := rule.New[rune, runes.Pos]("block", conc(text("{"), ws, many(stmt).SetPossessive(true), text("}"), ws, closing)).SetAtomic(true)
	limited := rule.New[rune, runes.Pos]("block", conc(ws, repetition.New[rune, runes.Pos](stmt, 0, 2).SetPossessive(true), closing))

	files := map[string]string{}

	for name, root := range map[string]pat{"B": block, "A": atomic, "L": limited} {
		src, err := codegen.Generate[rune, runes.Pos](root, cfg{Package: "probe", Name: name, Skip: stop})
		if err != nil {
			t.Fatal(err)
		}

		files[strings.ToLower(name)+".go"] = string(src)
	}

	files["stop_test.go"] = `package probe

import "testing"

func TestGarbageRightBeforeTheStop(t *testing.T) {
	tree, errs := NewBParser().ParseAll([]rune("{ let a = 1; ?? }"))
	if tree == nil || len(errs) != 1 {
		t.Fatalf("tree %v errors %v", tree != nil, errs)
	}

	if got := tree.Text(len(tree.Nodes) - 1); got != "?? " {
		t.Errorf("garbage %q", got)
	}
}

func TestAnAtomicRuleDoesNotSkip(t *testing.T) {
	// what is inside an atomic rule is not reported, and so there is nothing to recover from
	if tree, errs := NewAParser().ParseAll([]rune("{ let a = 1; ?? let b = 2; }")); tree != nil || len(errs) != 1 {
		t.Errorf("tree %v errors %v", tree != nil, errs)
	}
}

func TestARepetitionWithALimitDoesNotSkip(t *testing.T) {
	if tree, errs := NewLParser().ParseAll([]rune("let a = 1; ?? let b = 2;")); tree != nil || len(errs) != 1 {
		t.Errorf("tree %v errors %v", tree != nil, errs)
	}

	// and without garbage it is the same repetition
	if tree, errs := NewLParser().ParseAll([]rune("let a = 1; let b = 2;")); tree == nil || len(errs) != 0 {
		t.Errorf("tree %v errors %v", tree != nil, errs)
	}
}
`

	gentest.Run(t, files)
}
