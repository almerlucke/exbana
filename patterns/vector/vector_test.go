package vector_test

import (
	"bytes"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func input(s string) *runes.Reader {
	rd, err := runes.New(strings.NewReader(s))
	if err != nil {
		panic(err)
	}

	return rd
}

func word(s string) *vector.Vector[rune, runes.Pos] {
	return vector.New[rune, runes.Pos](func(a, b rune) bool { return a == b }, []rune(s)...)
}

func TestMatchesTheWholeSequence(t *testing.T) {
	rd := input("keyword!")
	ok, m, err := ebnf.Parse[rune, runes.Pos](word("key"), rd)
	if err != nil || !ok || m.End.Index != 3 || string(m.Value.([]rune)) != "key" {
		t.Fatalf("ok=%v m=%+v err=%v", ok, m, err)
	}
	if pos, _ := rd.Position(); pos.Index != 3 {
		t.Errorf("position = %d", pos.Index)
	}
}

func TestDoesNotMatchPartlyOrPastTheEnd(t *testing.T) {
	for _, in := range []string{"kex", "ke", "", "xkey"} {
		if ok, _, err := ebnf.Parse[rune, runes.Pos](word("key"), input(in)); ok || err != nil {
			t.Errorf("%q: ok=%v err=%v", in, ok, err)
		}
	}
}

func TestUsesTheEqualityFunctionItWasGiven(t *testing.T) {
	// case insensitive
	fold := vector.New[rune, runes.Pos](func(a, b rune) bool { return strings.EqualFold(string(a), string(b)) }, []rune("go")...)

	if ok, _, _ := ebnf.Parse[rune, runes.Pos](fold, input("GO")); !ok {
		t.Error("GO should match go")
	}
}

func TestGenerateWritesTheSequence(t *testing.T) {
	var out recorder
	if err := word("abc").Generate(ebnf.NewGenContext(1), &out); err != nil || string(out.runes) != "abc" {
		t.Errorf("out=%q err=%v", string(out.runes), err)
	}
}

func TestRecognizeNeedsTheSequenceAsAPrefix(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{{"key", true}, {"keys", true}, {"ke", false}, {"kex", false}, {"", false}} {
		matched, known := word("key").Recognize([]rune(c.in))
		if matched != c.want || !known {
			t.Errorf("Recognize(%q) = %v, %v", c.in, matched, known)
		}
	}
}

func TestPrintedText(t *testing.T) {
	var buf bytes.Buffer
	if err := word("key").Printed(`"key"`).Print(&buf); err != nil || buf.String() != `"key"` {
		t.Errorf("print = %q, %v", buf.String(), err)
	}
}

type recorder struct{ runes []rune }

func (r *recorder) Write(rs ...rune) error { r.runes = append(r.runes, rs...); return nil }
func (r *recorder) Finish() error          { return nil }
