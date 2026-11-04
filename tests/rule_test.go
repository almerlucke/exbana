package tests

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func TestRuleMatchEvalAndRecursion(t *testing.T) {
	digit := ebnf.Name(entR(func(r rune) bool { return r >= '0' && r <= '9' }), "digit")
	number := rule.New[rune, runes.Pos]("number", concR(digit, repetition.New[rune, runes.Pos](digit, 0, 0))).
		SetEval(func(m *ebnf.Match[rune, runes.Pos], rd ebnf.Reader[rune, runes.Pos]) (any, error) {
			rs, err := rd.Range(m.Begin, m.End)
			return len(rs), err
		})

	// nest = number | '(' nest ')', recursion through a forward declaration
	nest := rule.Declare[rune, runes.Pos]("nest")
	nest.Define(altR(number, concR(runeR('('), nest, runeR(')'))))

	rd, _ := runes.New(strings.NewReader("((123))"))
	ok, m, err := nest.Match(rd)
	if err != nil || !ok || m.ID() != "nest" {
		t.Fatalf("ok=%v id=%v err=%v", ok, m, err)
	}

	// the number match sits two parentheses deep and evaluates through its rule
	n := m.Components[0].Components[1]
	n = n.Components[0].Components[1]
	n = n.Components[0].Unpack()
	if n.ID() != "number" {
		t.Fatalf("got %q", n.ID())
	}
	if v, err := n.Eval(rd); err != nil || v != 3 {
		t.Fatalf("eval = %v, %v", v, err)
	}
}

func TestRuleUndefinedIsAnError(t *testing.T) {
	r := rule.Declare[rune, runes.Pos]("later")
	rd, _ := runes.New(strings.NewReader("x"))
	if _, _, err := r.Match(rd); err == nil || !strings.Contains(err.Error(), "later") {
		t.Fatalf("err = %v", err)
	}
	if r.Defined() {
		t.Fatal("should not be defined")
	}
}

func TestRulePrintAndGenerate(t *testing.T) {
	r := rule.New[rune, runes.Pos]("ab", concR(ebnf.Text(runeR('a'), "a"), ebnf.Text(runeR('b'), "b")))
	var buf bytes.Buffer
	if err := r.Print(&buf); err != nil || buf.String() != "(a, b)" {
		t.Fatalf("print = %q, %v", buf.String(), err)
	}
	if rules, err := ebnf.PrintRules([]ebnf.Pattern[rune, runes.Pos]{r}); err != nil || rules != "ab = (a, b)\n" {
		t.Fatalf("rules = %q, %v", rules, err)
	}
	buf.Reset()
	if err := concR(r, r).Print(&buf); err != nil || buf.String() != "(ab, ab)" {
		t.Fatalf("print as child = %q, %v", buf.String(), err)
	}
}

func TestRuleAtomicNamesTheRuleInSyntaxErrors(t *testing.T) {
	digit := ebnf.Name(entR(func(r rune) bool { return r >= '0' && r <= '9' }), "digit")
	sign := ebnf.Name(entR(func(r rune) bool { return r == '-' }), "minus")
	for _, atomic := range []bool{false, true} {
		number := rule.New[rune, runes.Pos]("number", concR(repetition.New[rune, runes.Pos](sign, 0, 1), digit)).SetAtomic(atomic)

		rd, _ := runes.New(strings.NewReader("x"))
		log := runes.NewFarthestLog(rd)
		ctx := ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log)
		if ok, _, _ := number.Match(ctx); ok {
			t.Fatal("should not match")
		}

		msg := log.Err().Error()
		if atomic && !strings.HasSuffix(msg, "expected number") {
			t.Fatalf("atomic: %s", msg)
		}
		if !atomic && !strings.Contains(msg, "digit") {
			t.Fatalf("non atomic: %s", msg)
		}
	}
}

func TestRuleGenerate(t *testing.T) {
	r := rule.New[rune, runes.Pos]("ab", concR(runeR('a'), runeR('b')))
	if got, err := gen(t, r, ebnf.NewGenContext(1)); err != nil || got != "ab" {
		t.Errorf("got %q, %v", got, err)
	}

	// a rule that is never defined can not generate
	if _, err := gen(t, rule.Declare[rune, runes.Pos]("later"), ebnf.NewGenContext(1)); err == nil || !strings.Contains(err.Error(), "later") {
		t.Errorf("err = %v", err)
	}

	// rules count towards the depth limit, so a recursive rule stops with ErrMaxDepth
	nest := rule.Declare[rune, runes.Pos]("nest")
	nest.Define(concR(runeR('('), nest, runeR(')')))
	ctx := ebnf.NewGenContext(1)
	ctx.MaxDepth = 10
	if _, err := gen(t, nest, ctx); !errors.Is(err, ebnf.ErrMaxDepth) {
		t.Errorf("expected ErrMaxDepth, got %v", err)
	}
	if ctx.Depth() != 0 {
		t.Errorf("depth not restored: %d", ctx.Depth())
	}
}

func TestRuleEvalFallsBackToTheWrappedPattern(t *testing.T) {
	inner := rule.New[rune, runes.Pos]("inner", runeR('a')).
		SetEval(func(m *ebnf.Match[rune, runes.Pos], rd ebnf.Reader[rune, runes.Pos]) (any, error) {
			return "evaluated", nil
		})
	outer := rule.New[rune, runes.Pos]("outer", inner)

	rd := newRunes("a")
	ok, m, err := ebnf.Parse[rune, runes.Pos](outer, rd)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if v, err := m.Eval(rd); err != nil || v != "evaluated" {
		t.Errorf("eval = %v, %v", v, err)
	}

	// nothing to evaluate with: the value of the match
	plain := rule.New[rune, runes.Pos]("plain", runeR('a'))
	rd = newRunes("a")
	_, m, _ = ebnf.Parse[rune, runes.Pos](plain, rd)
	if v, err := m.Eval(rd); err != nil || string(v.([]rune)) != "a" {
		t.Errorf("eval = %v, %v", v, err)
	}
}
