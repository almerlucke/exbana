package tests

import (
	"bytes"
	"io"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// anyOne is a pattern written against the three method core: it matches any single rune
type anyOne struct{}

func (anyOne) MatchCont(r ebnf.Reader[rune, runes.Pos], k ebnf.Continuation[rune, runes.Pos]) (bool, error) {
	return ebnf.Single(r, k, func(r ebnf.Reader[rune, runes.Pos]) (bool, *ebnf.Match[rune, runes.Pos], error) {
		begin, _ := r.Position()
		if _, err := r.Read1(); err != nil {
			return false, nil, nil
		}
		end, _ := r.Position()
		return true, ebnf.NewMatch[rune, runes.Pos](anyOne{}, begin, end, nil, nil), nil
	})
}

func (anyOne) Generate(_ *ebnf.GenContext, w ebnf.Writer[rune]) error { return w.Write('?') }

func (anyOne) Print(w io.Writer) error {
	_, err := io.WriteString(w, "any")
	return err
}

func TestCustomPatternNeedsOnlyThreeMethods(t *testing.T) {
	g := concR(anyOne{}, runeR('b'), anyOne{})

	ok, m, err := ebnf.Parse[rune, runes.Pos](g, newRunes("abc"))
	if err != nil || !ok || len(m.Components) != 3 {
		t.Fatalf("ok=%v m=%v err=%v", ok, m, err)
	}

	// no name, no evaluator: ID is empty and Eval gives the value
	if m.Components[0].ID() != "" {
		t.Error("unnamed pattern has an ID")
	}
	if v, err := m.Components[0].Eval(newRunes("abc")); err != nil || v == nil {
		t.Errorf("eval should give the (empty) value, got %v, %v", v, err)
	}

	var buf bytes.Buffer
	if err := g.Print(&buf); err != nil || buf.String() != "(any, , any)" {
		t.Errorf("print = %q, %v", buf.String(), err)
	}

	if ok, _, _ := ebnf.Parse[rune, runes.Pos](g, newRunes("abx")); !ok {
		t.Error("abx should match, the last pattern takes any rune")
	}
	if ok, _, _ := ebnf.Parse[rune, runes.Pos](g, newRunes("axc")); ok {
		t.Error("axc should not match")
	}
}
