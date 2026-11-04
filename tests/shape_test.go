package tests

import (
	"errors"
	"slices"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
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
	shape = ebnf.Shape[rune, runes.Pos]
	pat   = ebnf.Pattern[rune, runes.Pos]
)

func TestShapesOfTheBuiltInPatterns(t *testing.T) {
	a, b := runeR('a'), runeR('b')

	for name, c := range map[string]struct {
		p     pat
		check func(shape) bool
	}{
		"closure entity": {entR(func(rune) bool { return true }).Named("any").Printed("."),
			func(s shape) bool {
				return s.Kind == ebnf.KindClass && !s.Exact && s.Ranges == nil && s.ID == "any" && s.Text == "."
			}},
		"between": {entity.Between[rune, runes.Pos]('a', 'f'),
			func(s shape) bool {
				return s.Kind == ebnf.KindClass && s.Exact && !s.Negated && slices.Equal(s.Ranges, [][2]rune{{'a', 'f'}})
			}},
		"in": {entity.In[rune, runes.Pos](entity.Range[rune]{Lo: 'a', Hi: 'c'}, entity.Range[rune]{Lo: 'x', Hi: 'z'}),
			func(s shape) bool { return s.Kind == ebnf.KindClass && s.Exact && len(s.Ranges) == 2 }},
		"not in": {entity.NotIn[rune, runes.Pos](entity.Range[rune]{Lo: '"', Hi: '"'}),
			func(s shape) bool { return s.Kind == ebnf.KindClass && s.Exact && s.Negated && len(s.Ranges) == 1 }},
		"one of": {entity.OneOf[rune, runes.Pos]('+', '-'),
			func(s shape) bool {
				return s.Kind == ebnf.KindClass && s.Exact && slices.Equal(s.Ranges, [][2]rune{{'+', '+'}, {'-', '-'}})
			}},
		"equals": {entity.Equals[rune, runes.Pos]('x'),
			func(s shape) bool {
				return s.Kind == ebnf.KindLiteral && s.Exact && slices.Equal(s.Values, []rune{'x'})
			}},
		"any": {entity.Any[rune, runes.Pos](),
			func(s shape) bool { return s.Kind == ebnf.KindClass && s.Exact && s.Negated && len(s.Ranges) == 0 }},
		"closure vector": {vecR("abc"),
			func(s shape) bool { return s.Kind == ebnf.KindLiteral && !s.Exact && string(s.Values) == "abc" }},
		"literal": {vector.Literal[rune, runes.Pos]('a', 'b'),
			func(s shape) bool { return s.Kind == ebnf.KindLiteral && s.Exact && string(s.Values) == "ab" }},
		"text": {vector.Text[runes.Pos]("hi"),
			func(s shape) bool {
				return s.Kind == ebnf.KindLiteral && s.Exact && string(s.Values) == "hi" && s.Text == `"hi"`
			}},
		"end": {end.New[rune, runes.Pos]().Printed("eof"),
			func(s shape) bool { return s.Kind == ebnf.KindEnd && s.Text == "eof" }},
		"concatenation": {concatenation.New[rune, runes.Pos](a, b).Named("pair"),
			func(s shape) bool { return s.Kind == ebnf.KindSeq && len(s.Parts) == 2 && s.ID == "pair" }},
		"alternation": {alternation.New[rune, runes.Pos](a, b).SetOrthogonal(true),
			func(s shape) bool { return s.Kind == ebnf.KindAlt && len(s.Parts) == 2 && s.Orthogonal }},
		"repetition": {repetition.New[rune, runes.Pos](a, 2, 5).SetPossessive(true),
			func(s shape) bool {
				return s.Kind == ebnf.KindRepeat && s.Min == 2 && s.Max == 5 && s.Possessive && len(s.Parts) == 1
			}},
		"except": {except.New[rune, runes.Pos](a, b),
			func(s shape) bool {
				return s.Kind == ebnf.KindExcept && len(s.Parts) == 2 && s.Parts[0] == pat(a) && s.Parts[1] == pat(b)
			}},
		"rule": {rule.New[rune, runes.Pos]("r", a).SetLeftRecursive(true).SetAtomic(true),
			func(s shape) bool {
				return s.Kind == ebnf.KindRule && s.ID == "r" && s.LeftRecursive && s.Atomic && len(s.Parts) == 1
			}},
		"undefined rule": {rule.Declare[rune, runes.Pos]("later"),
			func(s shape) bool { return s.Kind == ebnf.KindRule && len(s.Parts) == 0 }},
	} {
		if s := ebnf.ShapeOf(c.p); !c.check(s) {
			t.Errorf("%s: shape %+v", name, s)
		}
	}
}

func TestPatternsFromScratchAreOpaque(t *testing.T) {
	s := ebnf.ShapeOf[rune, runes.Pos](anyOne{})
	if s.Kind != ebnf.KindOpaque || len(s.Parts) != 0 {
		t.Errorf("shape = %+v", s)
	}
}

func TestDeclarativeLeavesMatchAndGenerate(t *testing.T) {
	match := func(p pat, in string) bool {
		ok, _, err := ebnf.Parse(concR(p, endR()), newRunes(in))
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	letter := entity.In[rune, runes.Pos](entity.Range[rune]{Lo: 'a', Hi: 'z'}, entity.Range[rune]{Lo: 'A', Hi: 'Z'}, entity.Range[rune]{Lo: '_', Hi: '_'})
	for in, want := range map[string]bool{"a": true, "Q": true, "_": true, "1": false, "": false, "ab": false, "{": false} {
		if match(letter, in) != want {
			t.Errorf("letter %q: want %v", in, want)
		}
	}

	notQuote := entity.NotIn[rune, runes.Pos](entity.Range[rune]{Lo: '"', Hi: '"'}, entity.Range[rune]{Lo: '\\', Hi: '\\'})
	for in, want := range map[string]bool{"a": true, `"`: false, `\`: false, "": false} {
		if match(notQuote, in) != want {
			t.Errorf("notQuote %q: want %v", in, want)
		}
	}

	if !match(entity.OneOf[rune, runes.Pos]('+', '-'), "-") || match(entity.OneOf[rune, runes.Pos]('+', '-'), "*") {
		t.Error("OneOf")
	}
	if !match(entity.Any[rune, runes.Pos](), "x") || match(entity.Any[rune, runes.Pos](), "") {
		t.Error("Any")
	}
	if !match(entity.Between[rune, runes.Pos]('0', '9'), "7") || match(entity.Between[rune, runes.Pos]('0', '9'), "a") {
		t.Error("Between")
	}
	if !match(vector.Text[runes.Pos]("let"), "let") || match(vector.Text[runes.Pos]("let"), "lex") {
		t.Error("Text")
	}

	// they work with other element types than runes
	words := entity.Between[string, int]("b", "d")
	for w, want := range map[string]bool{"b": true, "cat": true, "d": true, "a": false, "e": false} {
		if m, _ := words.Recognize([]string{w}); m != want {
			t.Errorf("Between strings %q: %v", w, m)
		}
	}

	// the ones that name a value can generate it
	for _, p := range []pat{entity.Equals[rune, runes.Pos]('x'), vector.Text[runes.Pos]("xyz"), vector.Literal[rune, runes.Pos]('x', 'y', 'z')} {
		out, err := gen(t, p, ebnf.NewGenContext(1))
		if err != nil || !strings.HasPrefix("xyz", out) || out == "" {
			t.Errorf("generated %q, %v", out, err)
		}
	}

	// a class has to be told how to generate, as any entity does
	if _, err := gen(t, letter, ebnf.NewGenContext(1)); !errors.Is(err, ebnf.ErrNoGenerator) {
		t.Errorf("err = %v", err)
	}
}

func TestAnalyzeResolvesRulesByName(t *testing.T) {
	// list = "[" [ item { "," item } ] "]"   item = digit | list
	digit := entity.Between[rune, runes.Pos]('0', '9').Printed("digit")
	list := rule.Declare[rune, runes.Pos]("list")
	item := rule.New[rune, runes.Pos]("item", alternation.New[rune, runes.Pos](digit, list))
	list.Define(concatenation.New[rune, runes.Pos](
		vector.Text[runes.Pos]("["),
		repetition.Optional[rune, runes.Pos](concatenation.New[rune, runes.Pos](
			item, repetition.Any[rune, runes.Pos](concatenation.New[rune, runes.Pos](vector.Text[runes.Pos](","), item)))),
		vector.Text[runes.Pos]("]"),
	))

	g, err := ebnf.Analyze[rune, runes.Pos](list)
	if err != nil {
		t.Fatal(err)
	}

	if g.Root.Kind != ebnf.KindRef || g.Root.ID != "list" {
		t.Errorf("root = %v %q, a rule is described as a reference to it", g.Root.Kind, g.Root.ID)
	}
	if !slices.Equal(g.Order, []string{"list", "item"}) || len(g.Rules) != 2 {
		t.Errorf("order = %v, rules = %d", g.Order, len(g.Rules))
	}

	want := "list = (\"[\", (item, (\",\", item)*)?, \"]\")\nitem = ([0-9] | list)\n"
	if got := g.String(); got != want {
		t.Errorf("grammar:\n%s\nwant:\n%s", got, want)
	}

	// the table holds the definitions, the uses are references (the recursion is a finite structure)
	def := g.Rules["item"]
	if def.Kind != ebnf.KindRule || len(def.Children) != 1 || def.Children[0].Kind != ebnf.KindAlt {
		t.Fatalf("definition of item: %v", def)
	}
	if ref := def.Children[0].Children[1]; ref.Kind != ebnf.KindRef || ref.ID != "list" || len(ref.Children) != 0 {
		t.Errorf("use of list inside item: %v %q", ref.Kind, ref.ID)
	}
}

func TestAnalyzeAPatternThatIsNotARule(t *testing.T) {
	p := concatenation.New[rune, runes.Pos](vector.Text[runes.Pos]("a"), repetition.OneOrMore[rune, runes.Pos](entity.Between[rune, runes.Pos]('0', '9')))

	g, err := ebnf.Analyze[rune, runes.Pos](p)
	if err != nil {
		t.Fatal(err)
	}

	if g.Root.Kind != ebnf.KindSeq || len(g.Rules) != 0 || len(g.Root.Children) != 2 {
		t.Errorf("root = %v, rules = %d", g.Root.Kind, len(g.Rules))
	}
	if got := g.String(); got != "(\"a\", [0-9]+)\n" {
		t.Errorf("grammar = %q", got)
	}
}

func TestAnalyzeKeepsOpaquePatternsToCall(t *testing.T) {
	custom := anyOne{}
	g, err := ebnf.Analyze[rune, runes.Pos](concR(runeR('a'), custom))
	if err != nil {
		t.Fatal(err)
	}

	n := g.Root.Children[1]
	if n.Kind != ebnf.KindOpaque || n.Pattern != pat(custom) {
		t.Errorf("node = %v, pattern %v", n.Kind, n.Pattern)
	}
}

func TestAnalyzeReportsWhatCanNotBeDescribed(t *testing.T) {
	a := runeR('a')

	if _, err := ebnf.Analyze[rune, runes.Pos](concR(a, rule.Declare[rune, runes.Pos]("later"))); !errors.Is(err, ebnf.ErrUndefinedRule) || !strings.Contains(err.Error(), "later") {
		t.Errorf("undefined: %v", err)
	}

	one, other := rule.New[rune, runes.Pos]("same", a), rule.New[rune, runes.Pos]("same", runeR('b'))
	if _, err := ebnf.Analyze[rune, runes.Pos](concR(one, other)); !errors.Is(err, ebnf.ErrDuplicateRule) {
		t.Errorf("duplicate: %v", err)
	}

	// the same rule twice is fine
	if g, err := ebnf.Analyze[rune, runes.Pos](concR(one, one)); err != nil || len(g.Rules) != 1 {
		t.Errorf("same rule twice: %v", err)
	}

	// a pattern that contains itself without a named rule in between
	loop := concR()
	loop.SetPatterns(ebnf.Patterns[rune, runes.Pos]{a, loop})
	if _, err := ebnf.Analyze[rune, runes.Pos](loop); !errors.Is(err, ebnf.ErrUnnamedRecursion) {
		t.Errorf("recursion: %v", err)
	}

	// and through an unnamed rule
	r := rule.Declare[rune, runes.Pos]("")
	r.Define(concR(a, r))
	if _, err := ebnf.Analyze[rune, runes.Pos](r); !errors.Is(err, ebnf.ErrUnnamedRecursion) {
		t.Errorf("recursion through an unnamed rule: %v", err)
	}
}

func TestAnUnnamedRuleStandsForItsDefinition(t *testing.T) {
	g, err := ebnf.Analyze[rune, runes.Pos](rule.New[rune, runes.Pos]("", vector.Text[runes.Pos]("ab")))
	if err != nil {
		t.Fatal(err)
	}

	if g.Root.Kind != ebnf.KindLiteral || len(g.Rules) != 0 {
		t.Errorf("root = %v, rules = %d", g.Root.Kind, len(g.Rules))
	}
}

func TestAnalyzeAlsoWorksForOtherElementTypes(t *testing.T) {
	p := repetition.OneOrMore[int, int](entity.Between[int, int](0, 9))

	g, err := ebnf.Analyze[int, int](p)
	if err != nil {
		t.Fatal(err)
	}

	if g.String() != "[0-9]+\n" {
		t.Errorf("grammar = %q", g.String())
	}
}

func analyzed(t *testing.T, p pat) *ebnf.Grammar[rune, runes.Pos] {
	t.Helper()

	g, err := ebnf.Analyze[rune, runes.Pos](p)
	if err != nil {
		t.Fatal(err)
	}

	return g
}

func TestCanMatchNothing(t *testing.T) {
	a := vector.Text[runes.Pos]("a")
	digit := entity.Between[rune, runes.Pos]('0', '9')

	maybe := rule.New[rune, runes.Pos]("maybe", repetition.Optional[rune, runes.Pos](a))
	needs := rule.New[rune, runes.Pos]("needs", a)

	for name, c := range map[string]struct {
		p    pat
		want bool
	}{
		"literal":             {a, false},
		"empty literal":       {vector.Text[runes.Pos](""), true},
		"class":               {digit, false},
		"end":                 {end.New[rune, runes.Pos](), true},
		"sequence":            {concatenation.New[rune, runes.Pos](a, digit), false},
		"sequence of nothing": {concatenation.New[rune, runes.Pos](maybe, repetition.Any[rune, runes.Pos](a)), true},
		"alternation":         {alternation.New[rune, runes.Pos](a, maybe), true},
		"alternation of some": {alternation.New[rune, runes.Pos](a, digit), false},
		"star":                {repetition.Any[rune, runes.Pos](a), true},
		"plus":                {repetition.OneOrMore[rune, runes.Pos](a), false},
		"plus of nothing":     {repetition.OneOrMore[rune, runes.Pos](maybe), true},
		"except":              {except.New[rune, runes.Pos](a, digit), false},
		"rule":                {needs, false},
		"rule of nothing":     {maybe, true},
		"opaque":              {anyOne{}, true},
	} {
		g := analyzed(t, c.p)
		root := g.Root

		if root.Kind == ebnf.KindRef {
			root = g.Rules[root.ID].Children[0]
		}

		if got := g.CanMatchNothing(root); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

func TestLeftCallsAndCycles(t *testing.T) {
	x, y := vector.Text[runes.Pos]("x"), vector.Text[runes.Pos]("y")

	// a calls b before it has consumed anything, b calls c only after an x
	a, b, c := rule.Declare[rune, runes.Pos]("a"), rule.Declare[rune, runes.Pos]("b"), rule.Declare[rune, runes.Pos]("c")
	a.Define(concatenation.New[rune, runes.Pos](repetition.Optional[rune, runes.Pos](x), b, y))
	b.Define(concatenation.New[rune, runes.Pos](x, c))
	c.Define(y)

	g := analyzed(t, a)

	if got := g.LeftCalls(g.Rules["a"].Children[0]); !slices.Equal(got, []string{"b"}) {
		t.Errorf("a calls %v at its start", got)
	}
	if got := g.LeftCalls(g.Rules["b"].Children[0]); len(got) != 0 {
		t.Errorf("b calls %v at its start, but only after an x", got)
	}
	if got := g.LeftCycles(); len(got) != 0 {
		t.Errorf("cycles in a grammar without recursion: %v", got)
	}

	// indirect left recursion: p = q "x" | "y", q = p "z"
	p, q := rule.Declare[rune, runes.Pos]("p"), rule.Declare[rune, runes.Pos]("q")
	p.Define(alternation.New[rune, runes.Pos](concatenation.New[rune, runes.Pos](q, x), y))
	q.Define(concatenation.New[rune, runes.Pos](p, vector.Text[runes.Pos]("z")))

	if got := analyzed(t, p).LeftCycles(); !slices.Equal(got, []string{"p", "q"}) {
		t.Errorf("cycles = %v, want p and q", got)
	}

	// recursion after something was consumed is not left recursion
	r := rule.Declare[rune, runes.Pos]("r")
	r.Define(alternation.New[rune, runes.Pos](concatenation.New[rune, runes.Pos](x, r), y))

	if got := analyzed(t, r).LeftCycles(); len(got) != 0 {
		t.Errorf("right recursion reported as a cycle: %v", got)
	}

	// a directly recursive tail is the shape the generator turns into a loop, it is not a cycle
	e := rule.Declare[rune, runes.Pos]("e")
	e.Define(alternation.New[rune, runes.Pos](concatenation.New[rune, runes.Pos](e, x), y))

	if got := analyzed(t, e).LeftCycles(); len(got) != 0 {
		t.Errorf("a directly left recursive rule is a cycle: %v", got)
	}
}

func TestSplitLeftRecursive(t *testing.T) {
	x, y, z := vector.Text[runes.Pos]("x"), vector.Text[runes.Pos]("y"), vector.Text[runes.Pos]("z")

	build := func(define func(self *rule.Rule[rune, runes.Pos]) pat) *ebnf.Grammar[rune, runes.Pos] {
		self := rule.Declare[rune, runes.Pos]("r")
		self.Define(define(self))

		return analyzed(t, self)
	}

	// two bases and two tails
	g := build(func(r *rule.Rule[rune, runes.Pos]) pat {
		return alternation.New[rune, runes.Pos](x, concatenation.New[rune, runes.Pos](r, y), z, concatenation.New[rune, runes.Pos](r, z, x))
	})

	bases, tails, err := g.SplitLeftRecursive("r")
	if err != nil || len(bases) != 2 || len(tails) != 2 || tails[1].Children[0].ID != "r" || len(tails[1].Children) != 3 {
		t.Errorf("bases %d tails %d err %v", len(bases), len(tails), err)
	}

	// no tails: nothing is left recursive
	g = build(func(r *rule.Rule[rune, runes.Pos]) pat {
		return alternation.New[rune, runes.Pos](x, concatenation.New[rune, runes.Pos](y, r))
	})
	if bases, tails, err := g.SplitLeftRecursive("r"); err != nil || len(bases) != 2 || len(tails) != 0 {
		t.Errorf("right recursion: bases %d tails %d err %v", len(bases), len(tails), err)
	}

	for name, define := range map[string]func(r *rule.Rule[rune, runes.Pos]) pat{
		"only itself": func(r *rule.Rule[rune, runes.Pos]) pat { return alternation.New[rune, runes.Pos](x, r) },
		"no base":     func(r *rule.Rule[rune, runes.Pos]) pat { return concatenation.New[rune, runes.Pos](r, x) },
		"through a base": func(r *rule.Rule[rune, runes.Pos]) pat {
			return alternation.New[rune, runes.Pos](concatenation.New[rune, runes.Pos](concatenation.New[rune, runes.Pos](r, y), z), concatenation.New[rune, runes.Pos](r, x), x)
		},
	} {
		if _, _, err := build(define).SplitLeftRecursive("r"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}

	if _, _, err := build(func(r *rule.Rule[rune, runes.Pos]) pat { return x }).SplitLeftRecursive("nothing"); err == nil {
		t.Error("an unknown rule is not an error")
	}
}
