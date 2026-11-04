package slice_test

import (
	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
)

func newTwoStepGrammar() ebnf.Pattern[string, int] {
	is := func(s string) *ent.Entity[string, int] {
		return ent.New[string, int](func(x string) bool { return x == s })
	}

	return concatenation.New[string, int](is("a"), is("b"))
}
