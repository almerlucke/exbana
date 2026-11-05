package grammar

import (
	"slices"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/covenant/internal/samples"
	"github.com/almerlucke/exbana/v3/examples/covenant/token"
)

// accepts lexes and parses the source.
func accepts(t *testing.T, src string) bool {
	t.Helper()

	tokens, err := token.Lex(src)
	if err != nil {
		return false
	}

	return Accepts(token.Kinds(tokens))
}

func TestPrograms(t *testing.T) {
	for name, src := range samples.Accepted {
		t.Run(name, func(t *testing.T) {
			if !accepts(t, src) {
				t.Fatalf("does not parse:\n%s", src)
			}
		})
	}
}

func TestWhatIsNotCovenant(t *testing.T) {
	for name, src := range samples.Rejected {
		t.Run(name, func(t *testing.T) {
			if accepts(t, src) {
				t.Fatalf("parses, and should not:\n%s", src)
			}
		})
	}
}

// The grammar makes every choice by looking at the next token. If a change to the grammar breaks that, the optimizer says
// where, and the parser can not be generated.
func TestNoChoiceNeedsBacktracking(t *testing.T) {
	res, err := Analysis()
	if err != nil {
		t.Fatal(err)
	}

	if blocked := res.Blocked(); len(blocked) > 0 {
		t.Fatalf("the grammar backtracks in %d places:\n%v", len(blocked), res)
	}
}

func TestSourceCanBeGenerated(t *testing.T) {
	src, err := Source()
	if err != nil {
		t.Fatal(err)
	}

	if len(src) < 1000 {
		t.Fatalf("only %d bytes", len(src))
	}
}

// The grammar names the kinds it uses, so that an error can say what it expected.
func TestEveryKindIsNamed(t *testing.T) {
	for k := token.Kind(1); k.String() != "" && k < 200; k++ {
		if s := k.String(); len(s) > 6 && s[:6] == "token(" {
			break
		}

		if k.String() == "" {
			t.Errorf("kind %d has no name", k)
		}
	}

	if !slices.Contains(token.Keywords(), "trait") {
		t.Error("trait is not a keyword")
	}
}
