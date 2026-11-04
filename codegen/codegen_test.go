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

func TestRefusesLeftRecursionAndInexactLiterals(t *testing.T) {
	expr := rule.Declare[rune, runes.Pos]("expr").SetLeftRecursive(true)
	expr.Define(alt(conc(expr, text("-"), between('0', '9')), between('0', '9')).SetOrthogonal(true))

	_, err := codegen.Generate[rune, runes.Pos](expr, cfg{Package: "p"})
	if got := strings.Join(problems(t, err), "\n"); !strings.Contains(got, "rule expr is left recursive") {
		t.Errorf("problems:\n%s", got)
	}

	fold := vector.New[rune, runes.Pos](func(a, b rune) bool { return a == b }, 'a', 'b')
	_, err = codegen.Generate[rune, runes.Pos](fold, cfg{Package: "p"})
	if got := strings.Join(problems(t, err), "\n"); !strings.Contains(got, "compared with a function") {
		t.Errorf("problems:\n%s", got)
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
