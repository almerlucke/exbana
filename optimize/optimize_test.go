package optimize_test

import (
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

type pat = ebnf.Pattern[rune, runes.Pos]

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

// one analyses and returns the only decision the grammar has
func one(t *testing.T, p pat, opts optimize.Options) optimize.Decision[rune, runes.Pos] {
	t.Helper()

	res, err := optimize.Analyze[rune, runes.Pos](p, opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Decisions) != 1 {
		t.Fatalf("%d decisions: %v", len(res.Decisions), res)
	}

	return res.Decisions[0]
}

// decision returns the decision about the construct that prints as the text
func decision(t *testing.T, p pat, construct string, opts optimize.Options) optimize.Decision[rune, runes.Pos] {
	t.Helper()

	res, err := optimize.Analyze[rune, runes.Pos](p, opts)
	if err != nil {
		t.Fatal(err)
	}

	for _, d := range res.Decisions {
		if d.Construct == construct {
			return d
		}
	}

	t.Fatalf("no decision about %s in:\n%v", construct, res)

	return optimize.Decision[rune, runes.Pos]{}
}

func TestRepetitionFollowedByDifferentElementsCanBePossessive(t *testing.T) {
	// digit+ then "." : giving a digit back would leave a digit where a dot is needed
	d := one(t, conc(some(between('0', '9')), text(".")), optimize.Options{})

	if d.Kind != optimize.Possessive || !d.Safe {
		t.Errorf("decision = %+v", d)
	}
}

func TestRepetitionFollowedBySimilarElementsCanNot(t *testing.T) {
	// a* then "a": the repetition has to give its last a back
	d := one(t, conc(many(text("a")), text("a")), optimize.Options{})

	if d.Safe || !strings.Contains(d.Reason, "'a'") {
		t.Errorf("decision = %+v", d)
	}
}

func TestWhatFollowsCountsEvenWhenItCanMatchNothing(t *testing.T) {
	// letters* then [digit] then letters: the optional digit can be skipped, so what follows letters* includes letters again
	letters := between('a', 'z')
	d := decision(t, conc(many(letters), maybe(between('0', '9')), text("x")), "[a-z]*", optimize.Options{})

	// "x" is in a-z, so the repetition could have to give back an x
	if d.Safe {
		t.Errorf("decision = %+v", d)
	}

	// with a follow that is truly different it is safe
	d = decision(t, conc(many(between('a', 'f')), maybe(between('0', '9')), text("x")), "[a-f]*", optimize.Options{})
	if !d.Safe {
		t.Errorf("decision = %+v", d)
	}
}

func TestTheEndOfTheGrammarIsOpenUnlessItIsTheWholeInput(t *testing.T) {
	// a trailing repetition could be followed by anything when the grammar is used inside another one
	g := some(between('0', '9'))

	if d := one(t, g, optimize.Options{}); d.Safe || !strings.Contains(d.Reason, "Options.Closed") {
		t.Errorf("open: %+v", d)
	}
	if d := one(t, some(between('0', '9')), optimize.Options{Closed: true}); !d.Safe {
		t.Errorf("closed: %+v", d)
	}

	// an end pattern makes it closed without being told
	if d := one(t, conc(some(between('0', '9')), end.New[rune, runes.Pos]()), optimize.Options{}); !d.Safe {
		t.Errorf("with end: %+v", d)
	}
}

func TestAlternationWithBranchesThatStartDifferentlyCanBeOrthogonal(t *testing.T) {
	d := one(t, alt(text("if"), text("while"), between('0', '9')), optimize.Options{})
	if d.Kind != optimize.Orthogonal || !d.Safe {
		t.Errorf("decision = %+v", d)
	}

	// "if" and "in" both start with i
	d = one(t, alt(text("if"), text("in")), optimize.Options{})
	if d.Safe || !strings.Contains(d.Reason, "branches 1 and 2") || !strings.Contains(d.Reason, "'i'") {
		t.Errorf("decision = %+v", d)
	}
}

func TestAnAlternationBranchThatCanMatchNothingBlocks(t *testing.T) {
	d := decision(t, alt(text("a"), many(text("b"))), `("a" | "b"*)`, optimize.Options{})
	if d.Safe || !strings.Contains(d.Reason, "branch 2 can match nothing") {
		t.Errorf("decision = %+v", d)
	}
}

func TestABranchWithMoreThanOneWayToMatchBlocks(t *testing.T) {
	// the first branch is a* then "a", which needs backtracking, so committing to it would be wrong
	first := conc(many(text("a")), text("a"))
	g := alt(first, text("b"))

	res, err := optimize.Analyze[rune, runes.Pos](g, optimize.Options{})
	if err != nil {
		t.Fatal(err)
	}

	var altDecision *optimize.Decision[rune, runes.Pos]
	for i, d := range res.Decisions {
		if d.Kind == optimize.Orthogonal {
			altDecision = &res.Decisions[i]
		}
	}

	if altDecision == nil || altDecision.Safe || !strings.Contains(altDecision.Reason, "branch 1 can match in more than one way") {
		t.Errorf("decisions:\n%v", res)
	}
}

func TestFunctionsMakeTheAnalysisGiveUpAndSayWhy(t *testing.T) {
	opaque := entity.New[rune, runes.Pos](func(r rune) bool { return r == 'x' })

	d := one(t, conc(many(opaque), text("y")), optimize.Options{})
	if d.Safe || !strings.Contains(d.Reason, "can not look into") {
		t.Errorf("decision = %+v", d)
	}
}

func TestRulesAndRecursion(t *testing.T) {
	// list = "[" [ item { "," item } ] "]"   item = digit+ | list
	digits := some(between('0', '9'))
	list := rule.Declare[rune, runes.Pos]("list")
	item := rule.New[rune, runes.Pos]("item", alt(digits, list))
	list.Define(conc(text("["), maybe(conc(item, many(conc(text(","), item)))), text("]")))

	res, err := optimize.Analyze[rune, runes.Pos](list, optimize.Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, d := range res.Decisions {
		if !d.Safe {
			t.Errorf("not safe: %s %s: %s", d.Kind, d.Construct, d.Reason)
		}
	}

	if len(res.Decisions) != 4 {
		t.Errorf("%d decisions:\n%v", len(res.Decisions), res)
	}

	// the repetition of the items is followed by "]" in list, and what follows item itself is "," or "]": never a digit or "["
	for _, d := range res.Decisions {
		if d.Rule == "" {
			t.Errorf("a decision without a rule: %+v", d)
		}
	}
}

func TestAPatternUsedInTwoPlacesIsOnlyChangedIfItIsSafeInBoth(t *testing.T) {
	digits := some(between('0', '9'))

	// followed by "." here, which is fine, and by a digit there, which is not
	g := alt(conc(digits, text(".")), conc(digits, between('0', '9')))

	res, err := optimize.Analyze[rune, runes.Pos](g, optimize.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if changed := res.Apply(); changed != 0 {
		t.Errorf("%d patterns changed, the shared repetition is not safe in the second place:\n%v", changed, res)
	}

	if digits.Possessive() {
		t.Error("the shared repetition was made possessive")
	}
}

func loopDecision(t *testing.T, g pat) optimize.Decision[rune, runes.Pos] {
	t.Helper()

	res, err := optimize.Analyze[rune, runes.Pos](g, optimize.Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, d := range res.Decisions {
		if d.Kind == optimize.Loop {
			return d
		}
	}

	t.Fatalf("no decision about a loop in:\n%v", res)

	return optimize.Decision[rune, runes.Pos]{}
}

func leftRecursive(tail string) *rule.Rule[rune, runes.Pos] {
	// expr = expr tail digit | digit
	expr := rule.Declare[rune, runes.Pos]("expr").SetLeftRecursive(true)
	expr.Define(alt(conc(expr, text(tail), between('0', '9')), between('0', '9')))

	return expr
}

func TestALeftRecursiveRuleCanBeALoopWhenWhatItAddsCanNotBeWhatFollowsIt(t *testing.T) {
	// expr = expr "-" digit | digit, then ";": the tails start with "-", and a ";" follows
	d := loopDecision(t, conc(leftRecursive("-"), text(";")))
	if d.Kind != optimize.Loop || !d.Safe || d.Rule != "expr" {
		t.Errorf("decision = %+v", d)
	}

	// the same rule, but a "-" can follow it: giving back a tail could let the "-" that follows take it
	d = loopDecision(t, conc(leftRecursive("-"), text("-x")))
	if d.Safe || !strings.Contains(d.Reason, "what a tail starts with and what follows the rule") || !strings.Contains(d.Reason, "'-'") {
		t.Errorf("decision = %+v", d)
	}

	// and when it is the whole grammar, nothing follows
	if d := loopDecision(t, leftRecursive("-")); d.Safe {
		t.Errorf("an open grammar may be followed by anything: %+v", d)
	}

	res, _ := optimize.Analyze[rune, runes.Pos](leftRecursive("-"), optimize.Options{Closed: true})
	if len(res.Blocked()) != 0 {
		t.Errorf("a closed grammar:\n%v", res)
	}
}

func TestALeftRecursiveRuleThatIsNotALoop(t *testing.T) {
	x, y := text("x"), text("y")

	for name, c := range map[string]struct {
		define func(r *rule.Rule[rune, runes.Pos]) pat
		reason string
	}{
		"a tail that can match nothing": {func(r *rule.Rule[rune, runes.Pos]) pat { return alt(conc(r, many(x)), y) }, "can match nothing"},
		"tails that start the same":     {func(r *rule.Rule[rune, runes.Pos]) pat { return alt(conc(r, x, y), conc(r, x), y) }, "tails 1 and 2"},
		"bases that start the same":     {func(r *rule.Rule[rune, runes.Pos]) pat { return alt(text("ab"), text("ac"), conc(r, x)) }, "base branches 1 and 2"},
		"no base":                       {func(r *rule.Rule[rune, runes.Pos]) pat { return conc(r, x) }, "nothing for it to start from"},
		"a base that matches nothing":   {func(r *rule.Rule[rune, runes.Pos]) pat { return alt(many(y), conc(r, x)) }, "base branch 1 can match nothing"},
		// a and ab both match "ab": which one is used depends on what follows, so the loop would have to try them
		"a tail that matches in more than one way": {func(r *rule.Rule[rune, runes.Pos]) pat {
			return alt(conc(r, alt(text("a"), text("ab"))), y)
		}, "tail 1 can match in more than one way"},
		"a base that matches in more than one way": {func(r *rule.Rule[rune, runes.Pos]) pat {
			return alt(alt(text("a"), text("ab")), conc(r, x))
		}, "base branch 1 can match in more than one way"},
	} {
		r := rule.Declare[rune, runes.Pos]("r").SetLeftRecursive(true)
		r.Define(c.define(r))

		d := loopDecision(t, conc(r, text(";")))
		if d.Safe || !strings.Contains(d.Reason, c.reason) {
			t.Errorf("%s: %+v", name, d)
		}
	}
}

func TestInsideALeftRecursiveRuleOnlyItsBasesAndTailsAreLookedAt(t *testing.T) {
	// the top alternation of a left recursive rule is not an alternation to commit to, it is the loop: there is no decision for
	// it, but the constructs inside the bases and the tails have theirs
	expr := rule.Declare[rune, runes.Pos]("expr").SetLeftRecursive(true)
	expr.Define(alt(conc(expr, text("-"), some(between('0', '9'))), some(between('0', '9'))))

	res, err := optimize.Analyze[rune, runes.Pos](conc(expr, text(";")), optimize.Options{})
	if err != nil {
		t.Fatal(err)
	}

	kinds := map[optimize.Kind]int{}
	for _, d := range res.Decisions {
		kinds[d.Kind]++
	}

	if kinds[optimize.Loop] != 1 || kinds[optimize.Orthogonal] != 0 || kinds[optimize.Possessive] != 2 {
		t.Errorf("decisions by kind %v:\n%v", kinds, res)
	}

	// applying changes the repetitions and leaves the rule as it is
	if changed := res.Apply(); changed != 2 {
		t.Errorf("%d patterns changed", changed)
	}
}

func TestWithoutKnowingTheElementsLittleCanBeProven(t *testing.T) {
	// elements that can not be mapped to integers: every leaf could accept anything
	g := conc(some(between('0', '9')), text(";"))

	res, err := optimize.AnalyzeFunc[rune, runes.Pos](g, optimize.Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Safe()) != 0 || len(res.Blocked()) != 1 || !strings.Contains(res.Blocked()[0].Reason, "can not look into") {
		t.Errorf("result:\n%v", res)
	}

	// with the mapping it is provable
	res, _ = optimize.AnalyzeFunc[rune, runes.Pos](g, optimize.Options{}, func(r rune) int64 { return int64(r) })
	if len(res.Safe()) != 1 {
		t.Errorf("result:\n%v", res)
	}
}

func TestApplyChangesThePatterns(t *testing.T) {
	digits := some(between('0', '9'))
	choice := alt(text("a"), text("b"))
	g := conc(digits, choice, text("."))

	res, err := optimize.Optimize[rune, runes.Pos](g, optimize.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if !digits.Possessive() {
		t.Error("the repetition was not made possessive")
	}
	if len(res.Safe()) != 2 || len(res.Blocked()) != 0 {
		t.Errorf("result:\n%v", res)
	}

	// and a second pass has nothing left to do
	again, _ := optimize.Analyze[rune, runes.Pos](g, optimize.Options{})
	if len(again.Decisions) != 0 {
		t.Errorf("second pass found %d decisions", len(again.Decisions))
	}

	// it still matches
	rd, _ := runes.New(strings.NewReader("123b."))
	if ok, _, _ := ebnf.Parse[rune, runes.Pos](g, rd); !ok {
		t.Error("the optimized grammar does not match")
	}
}

func TestReportSaysWhatWasFound(t *testing.T) {
	res, _ := optimize.Analyze[rune, runes.Pos](conc(some(between('0', '9')), text("."), many(text("a")), text("a")), optimize.Options{})

	r := res.String()
	for _, want := range []string{"1 can be made possessive or orthogonal, 1 can not", "safe possessive", "not safe possessive", "'a'"} {
		if !strings.Contains(r, want) {
			t.Errorf("report does not contain %q:\n%s", want, r)
		}
	}
}

func TestErrorsFromAnalyzeArePassedOn(t *testing.T) {
	if _, err := optimize.Analyze[rune, runes.Pos](rule.Declare[rune, runes.Pos]("later"), optimize.Options{}); err == nil {
		t.Error("expected an error for an undefined rule")
	}
}

func TestWorksForOtherIntegerElementTypes(t *testing.T) {
	// bytes: a run of 0..9 then 10
	g := concatenation.New[byte, int](
		repetition.OneOrMore[byte, int](entity.Between[byte, int](0, 9)),
		entity.Equals[byte, int](10),
	)

	res, err := optimize.Analyze[byte, int](g, optimize.Options{})
	if err != nil || len(res.Decisions) != 1 || !res.Decisions[0].Safe {
		t.Errorf("res=%v err=%v", res, err)
	}
}

func TestASequenceStartsWithWhatFollowsItsPartsThatCanMatchNothing(t *testing.T) {
	// (a? b) can start with a or with b. The second branch "bc" starts with b too, so they are not exclusive: on "bc" the
	// alternation prefers the longer second branch, and an orthogonal one would commit to the first and match only "b".
	g := alt(conc(maybe(text("a")), text("b")), text("bc"))

	d := decision(t, g, `(("a"?, "b") | "bc")`, optimize.Options{})
	if d.Safe || !strings.Contains(d.Reason, "branches 1 and 2") || !strings.Contains(d.Reason, "'b'") {
		t.Errorf("decision = %+v", d)
	}

	// and the real thing: the original prefers "bc", which is why it must not be changed
	rd, _ := runes.New(strings.NewReader("bc"))
	ok, m, _ := ebnf.Parse[rune, runes.Pos](g, rd)
	if !ok || m.End.Index != 2 {
		t.Fatalf("ok=%v m=%+v", ok, m)
	}
}

func TestWhatFollowsASequenceIncludesWhatComesAfterItsLastPartsThatCanMatchNothing(t *testing.T) {
	// x* then (y? z?) then "x": the x* can be followed by y, z or x, so it can not give nothing back
	g := conc(many(text("x")), maybe(text("y")), maybe(text("z")), text("x"))

	d := decision(t, g, `"x"*`, optimize.Options{})
	if d.Safe || !strings.Contains(d.Reason, "'x'") {
		t.Errorf("decision = %+v", d)
	}
}

func TestOptionalIsNotRepeated(t *testing.T) {
	// a? then "a" can not be possessive (the a may have to be given back) but (b)? then "b"... the same, whereas
	// (a)? then "c" can: an optional item is followed by what follows it, not by itself
	if d := decision(t, conc(maybe(text("a")), text("c")), `"a"?`, optimize.Options{}); !d.Safe {
		t.Errorf("decision = %+v", d)
	}
	if d := decision(t, conc(maybe(text("a")), text("a")), `"a"?`, optimize.Options{}); d.Safe {
		t.Errorf("decision = %+v", d)
	}
}

func endPattern() pat { return end.New[rune, runes.Pos]().Printed("end of input") }
