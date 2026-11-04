package slice_test

import (
	"fmt"
	"strings"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/readers/slice"
)

// Parse a stream of words rather than characters: a command is a verb, then any number of nouns.
func ExampleNew() {
	word := func(words ...string) *ent.Entity[string, int] {
		return ent.New[string, int](func(w string) bool {
			for _, c := range words {
				if w == c {
					return true
				}
			}

			return false
		}).Printed(strings.Join(words, " or "))
	}

	command := concatenation.New[string, int](
		alternation.New[string, int](word("take"), word("drop")),
		repetition.OneOrMore[string, int](word("lamp", "key", "sword")),
		end.New[string, int]().Printed("end of command"),
	)

	for _, words := range [][]string{
		{"take", "lamp", "key"},
		{"drop", "sword"},
		{"take"},
		{"look", "lamp"},
	} {
		rd := slice.New(words)
		log := slice.NewFarthestLog(rd, func(w string) string { return fmt.Sprintf("%q", w) })

		ok, _, _ := ebnf.Parse[string, int](command, ebnf.NewParseContext[string, int](rd).WithLogger(log))
		if ok {
			fmt.Println(strings.Join(words, " "), "-> ok")
			continue
		}

		fmt.Println(strings.Join(words, " "), "->", log.Err())
	}

	// Output:
	// take lamp key -> ok
	// drop sword -> ok
	// take -> syntax error at element 1: unexpected end of input, expected lamp or key or sword
	// look lamp -> syntax error at element 0: unexpected "look", expected take or drop
}
