package tests

import (
	"errors"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// Running out of stack is a fatal error in Go, the process dies and nothing can be recovered. The matcher has to notice
// it is getting close and report an error instead. These tests use hundreds of megabytes of stack on purpose.

func TestLongRepetitionReportsTooDeepInsteadOfCrashing(t *testing.T) {
	if testing.Short() {
		t.Skip("uses a few hundred megabytes of stack")
	}

	digit := ebnf.Name(entR(func(r rune) bool { return r >= '0' && r <= '9' }), "digit")
	num := rule.New[rune, runes.Pos]("num", repR(digit, 1, 0).SetPossessive(true))
	item := rule.New[rune, runes.Pos]("item", concR(
		repR(runeR(' '), 0, 0).SetPossessive(true),
		altR(num, concR(runeR('('), num, runeR(')'))),
		runeR(','),
	))
	g := concR(repR(item, 0, 0), endR())

	// the same grammar matches a shorter input, so it is the length that is the problem
	if ok, _, err := ebnf.Parse[rune, runes.Pos](g, newRunes(strings.Repeat(" 12,", 1000))); !ok || err != nil {
		t.Fatalf("short input: ok=%v err=%v", ok, err)
	}

	// without the check this is a fatal "goroutine stack exceeds 1000000000-byte limit"
	_, _, err := ebnf.Parse[rune, runes.Pos](g, newRunes(strings.Repeat(" 12,", 600_000)))
	if !errors.Is(err, ebnf.ErrTooDeep) {
		t.Fatalf("err = %v, want ErrTooDeep", err)
	}

	// and the advice in the error works: a possessive repetition does not recurse
	g = concR(repR(item, 0, 0).SetPossessive(true), endR())
	if ok, _, err := ebnf.Parse[rune, runes.Pos](g, newRunes(strings.Repeat(" 12,", 600_000))); !ok || err != nil {
		t.Fatalf("possessive: ok=%v err=%v", ok, err)
	}
}

func TestDeeplyNestedRulesReportTooDeepInsteadOfCrashing(t *testing.T) {
	if testing.Short() {
		t.Skip("uses a few hundred megabytes of stack")
	}

	// nest = "(" nest ")" | "x"
	nest := rule.Declare[rune, runes.Pos]("nest")
	nest.Define(altR(concR(runeR('('), nest, runeR(')')), runeR('x')))

	depth := func(n int) string { return strings.Repeat("(", n) + "x" + strings.Repeat(")", n) }

	if ok, _, err := ebnf.Parse[rune, runes.Pos](nest, newRunes(depth(1000))); !ok || err != nil {
		t.Fatalf("depth 1000: ok=%v err=%v", ok, err)
	}

	_, _, err := ebnf.Parse[rune, runes.Pos](nest, newRunes(depth(2_000_000)))
	if !errors.Is(err, ebnf.ErrTooDeep) {
		t.Fatalf("err = %v, want ErrTooDeep", err)
	}
}

func TestCheckStackIsCheapAndQuietWhenShallow(t *testing.T) {
	for _, d := range []int{0, 1, 4095, 4096, 8192, 100_000} {
		if err := ebnf.CheckStack(d); err != nil {
			t.Errorf("depth %d: %v", d, err)
		}
	}
}
