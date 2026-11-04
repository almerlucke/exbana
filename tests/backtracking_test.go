package tests

import (
	"strings"
	"testing"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func TestBacktracking(t *testing.T) {
	// (a)* , a
	// Greedily (a)* will take all 'a's, but then 'a' will fail.
	// Backtracking should make (a)* give back one 'a'.

	aStar := repetition.Any[rune, runes.Pos](runeR('a'))
	c := concR(aStar, runeR('a'))

	rd, _ := runes.New(strings.NewReader("aaa"))
	matched, result, err := c.Match(rd)
	if err != nil || !matched {
		t.Fatalf("expected match with backtracking, got %v, %v", matched, err)
	}

	if len(result.Components) != 2 {
		t.Errorf("expected 2 main components, got %d", len(result.Components))
	}

	starMatch := result.Components[0]
	if len(starMatch.Components) != 2 {
		t.Errorf("expected (a)* to match 2 'a's after backtracking, got %d", len(starMatch.Components))
	}
}

func TestBacktrackingDeep(t *testing.T) {
	cases := []struct {
		name    string
		pattern ebnf.Pattern[rune, runes.Pos]
		input   string
		want    bool
	}{
		{"two repetitions", concR(anyR(runeR('a')), anyR(runeR('a')), runeR('b')), "aab", true},
		{"repetition inside alternation", concR(altR(anyR(runeR('a'))), runeR('a')), "aa", true},
		{"alternation longest first then shorter", concR(altR(vecR("a"), vecR("ab")), vecR("bc")), "abc", true},
		{"repetition of alternation", concR(anyR(altR(vecR("a"), vecR("ab"))), vecR("bc")), "aabc", true},
		{"nested concatenation", concR(concR(anyR(runeR('a')), runeR('b')), anyR(runeR('b')), runeR('b')), "abbb", true},
		{"except inside repetition", concR(anyR(exceptR(entR(unicode.IsLetter), runeR('x'))), runeR('x')), "abx", true},
		{"still fails", concR(anyR(runeR('a')), runeR('b')), "aac", false},
		{"min is respected", concR(repR(runeR('a'), 2, 0), runeR('a')), "aa", false},
		{"max is respected", concR(repR(runeR('a'), 0, 2), runeR('a'), endR()), "aaaa", false},
	}

	for _, c := range cases {
		matched, result, err := ebnf.Parse(c.pattern, newRunes(c.input))
		if err != nil || matched != c.want {
			t.Errorf("%s: got %v, %v, want %v", c.name, matched, err, c.want)
			continue
		}
		if matched && result.End.Index != len(c.input) && c.name != "max is respected" {
			t.Errorf("%s: match ended at %d, want %d", c.name, result.End.Index, len(c.input))
		}
	}
}

func TestPossessiveAndOrthogonalDoNotBacktrack(t *testing.T) {
	p := concR(anyR(runeR('a')).SetPossessive(true), runeR('a'))
	if matched, _, _ := p.Match(newRunes("aaa")); matched {
		t.Error("possessive repetition gave back a match")
	}

	o := concR(altR(vecR("ab"), vecR("a")).SetOrthogonal(true), vecR("bc"))
	if matched, _, _ := o.Match(newRunes("abc")); matched {
		t.Error("orthogonal alternation backtracked")
	}
}

func TestLongNonPossessiveRepetition(t *testing.T) {
	in := strings.Repeat("a", 200000)
	matched, _, err := concR(anyR(runeR('a')), runeR('a')).Match(newRunes(in))
	if err != nil || !matched {
		t.Errorf("got %v, %v", matched, err)
	}
}
