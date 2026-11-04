package rule_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func word(s string) *vector.Vector[rune, runes.Pos] {
	return vector.New[rune, runes.Pos](func(a, b rune) bool { return a == b }, []rune(s)...).Printed(`"` + s + `"`)
}

func TestMatchHasTheRuleAsItsPatternAndTheComponentsOfTheDefinition(t *testing.T) {
	r := rule.New[rune, runes.Pos]("pair", concatenation.New[rune, runes.Pos](word("a"), word("b")))

	ok, m, err := ebnf.Parse[rune, runes.Pos](r, input("ab"))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}

	if m.ID() != "pair" || m.Pattern != ebnf.Pattern[rune, runes.Pos](r) {
		t.Errorf("match is of %q", m.ID())
	}
	if len(m.Components) != 2 {
		t.Errorf("components = %d, want the two of the concatenation", len(m.Components))
	}
	// a rule is named, so Unpack stops at it
	if m.Unpack() != m {
		t.Error("Unpack looked through a rule")
	}
}

func TestEvalUsesTheRulesFunctionThenTheWrappedPatternThenTheValue(t *testing.T) {
	rd := input("a")

	withFunc := rule.New[rune, runes.Pos]("f", word("a")).
		SetEval(func(*ebnf.Match[rune, runes.Pos], ebnf.Reader[rune, runes.Pos]) (any, error) {
			return "from the rule", nil
		})
	_, m, _ := ebnf.Parse[rune, runes.Pos](withFunc, rd)
	if v, _ := m.Eval(rd); v != "from the rule" {
		t.Errorf("eval = %v", v)
	}

	outer := rule.New[rune, runes.Pos]("outer", withFunc)
	_, m, _ = ebnf.Parse[rune, runes.Pos](outer, input("a"))
	if v, _ := m.Eval(rd); v != "from the rule" {
		t.Errorf("eval through a wrapped rule = %v", v)
	}

	plain := rule.New[rune, runes.Pos]("plain", word("a"))
	_, m, _ = ebnf.Parse[rune, runes.Pos](plain, input("a"))
	if v, _ := m.Eval(rd); string(v.([]rune)) != "a" {
		t.Errorf("eval = %v", v)
	}
}

func TestUndefinedRule(t *testing.T) {
	r := rule.Declare[rune, runes.Pos]("later")

	if r.Defined() {
		t.Error("Defined before Define")
	}

	_, _, err := ebnf.Parse[rune, runes.Pos](r, input("a"))
	if err == nil || !strings.Contains(err.Error(), `"later"`) {
		t.Errorf("err = %v", err)
	}

	if err := r.Generate(ebnf.NewGenContext(1), &recorder{}); err == nil {
		t.Error("generated from an undefined rule")
	}

	r.Define(word("a"))
	if ok, _, err := ebnf.Parse[rune, runes.Pos](r, input("a")); !ok || err != nil || !r.Defined() {
		t.Errorf("after Define: ok=%v err=%v", ok, err)
	}
}

func TestAtomicRuleIsReportedAsAWhole(t *testing.T) {
	digit := entity.New[rune, runes.Pos](func(r rune) bool { return r >= '0' && r <= '9' }).Named("digit")
	number := rule.New[rune, runes.Pos]("number", concatenation.New[rune, runes.Pos](digit, digit))

	for atomic, want := range map[bool]string{false: "digit", true: "number"} {
		number.SetAtomic(atomic)

		rd := input("x")
		log := runes.NewFarthestLog(rd)
		ebnf.Parse[rune, runes.Pos](number, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))

		if e := log.Err(); e == nil || !strings.HasSuffix(e.Error(), "expected "+want) {
			t.Errorf("atomic=%v: %v", atomic, e)
		}
	}
}

func TestUnmarkedRecursionAtTheSamePositionIsAnError(t *testing.T) {
	r := rule.Declare[rune, runes.Pos]("r")
	r.Define(alternation.New[rune, runes.Pos](concatenation.New[rune, runes.Pos](r, word("a")), word("a")))

	_, _, err := ebnf.Parse[rune, runes.Pos](r, input("aa"))
	if !errors.Is(err, rule.ErrLeftRecursion) || !strings.Contains(err.Error(), `"r"`) {
		t.Errorf("err = %v", err)
	}
}

func TestMarkedRuleOffersItsMatchesLongestFirst(t *testing.T) {
	r := rule.Declare[rune, runes.Pos]("r").SetLeftRecursive(true)
	r.Define(alternation.New[rune, runes.Pos](concatenation.New[rune, runes.Pos](r, word("a")), word("a")))

	var ends []int
	for m, err := range ebnf.Matches[rune, runes.Pos](r, input("aaab")) {
		if err != nil {
			t.Fatal(err)
		}
		ends = append(ends, m.End.Index)
	}

	if len(ends) != 3 || ends[0] != 3 || ends[1] != 2 || ends[2] != 1 {
		t.Errorf("ends = %v, want [3 2 1]", ends)
	}
}

func TestChildrenPrintAndGenerate(t *testing.T) {
	inner := word("ab")
	r := rule.New[rune, runes.Pos]("r", inner)

	if c := r.Children(); len(c) != 1 || c[0] != ebnf.Pattern[rune, runes.Pos](inner) {
		t.Errorf("children = %v", c)
	}
	if len(rule.Declare[rune, runes.Pos]("x").Children()) != 0 {
		t.Error("undefined rule has children")
	}

	// a rule prints its definition, and inside another pattern just its name
	var buf bytes.Buffer
	if err := r.Print(&buf); err != nil || buf.String() != `"ab"` {
		t.Errorf("print = %q, %v", buf.String(), err)
	}
	buf.Reset()
	if err := concatenation.New[rune, runes.Pos](r, r).Print(&buf); err != nil || buf.String() != "(r, r)" {
		t.Errorf("print as child = %q, %v", buf.String(), err)
	}

	var w recorder
	if err := r.Generate(ebnf.NewGenContext(1), &w); err != nil || string(w.runes) != "ab" {
		t.Errorf("generate = %q, %v", string(w.runes), err)
	}
}

type recorder struct{ runes []rune }

func (r *recorder) Write(rs ...rune) error { r.runes = append(r.runes, rs...); return nil }
func (r *recorder) Finish() error          { return nil }
