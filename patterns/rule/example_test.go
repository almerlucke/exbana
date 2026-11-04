package rule_test

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

type (
	match  = ebnf.Match[rune, runes.Pos]
	reader = ebnf.Reader[rune, runes.Pos]
)

func char(c rune) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](func(r rune) bool { return r == c }).Printed(string(c))
}

func input(s string) *runes.Reader {
	rd, err := runes.New(strings.NewReader(s))
	if err != nil {
		panic(err)
	}

	return rd
}

func number() *rule.Rule[rune, runes.Pos] {
	digit := ent.New[rune, runes.Pos](unicode.IsDigit).Named("digit")

	return rule.New[rune, runes.Pos]("number", repetition.OneOrMore[rune, runes.Pos](digit)).
		SetEval(func(m *match, rd reader) (any, error) {
			text, err := rd.Range(m.Begin, m.End)
			if err != nil {
				return nil, err
			}

			return strconv.Atoi(string(text))
		})
}

// A rule can be declared before it is defined, which is how a recursive grammar is written: a list is numbers
// between brackets, and a list can be an item of a list.
func ExampleDeclare() {
	list := rule.Declare[rune, runes.Pos]("list")
	item := alternation.New[rune, runes.Pos](number(), list)

	list.Define(concatenation.New[rune, runes.Pos](
		char('['),
		repetition.Optional[rune, runes.Pos](concatenation.New[rune, runes.Pos](
			item,
			repetition.Any[rune, runes.Pos](concatenation.New[rune, runes.Pos](char(','), item)),
		)),
		char(']'),
	))

	for _, in := range []string{"[]", "[1,2,3]", "[1,[2,[3]],4]", "[1,", "[1 2]"} {
		ok, _, _ := ebnf.Parse[rune, runes.Pos](concatenation.New[rune, runes.Pos](list, end.New[rune, runes.Pos]()), input(in))
		fmt.Printf("%-14s %v\n", in, ok)
	}

	// Output:
	// []             true
	// [1,2,3]        true
	// [1,[2,[3]],4]  true
	// [1,            false
	// [1 2]          false
}

// A left recursive rule starts with a call to itself. Marked with SetLeftRecursive it grows its matches from the
// non-recursive ones, which gives left associative trees: 10-3-2 is (10-3)-2.
func ExampleRule_SetLeftRecursive() {
	expr := rule.Declare[rune, runes.Pos]("expr").SetLeftRecursive(true).
		SetEval(func(m *match, rd reader) (any, error) { return m.Components[0].Eval(rd) })

	difference := rule.New[rune, runes.Pos]("difference", concatenation.New[rune, runes.Pos](expr, char('-'), number())).
		SetEval(func(m *match, rd reader) (any, error) {
			l, err := m.Components[0].Eval(rd)
			if err != nil {
				return nil, err
			}

			r, err := m.Components[2].Eval(rd)
			if err != nil {
				return nil, err
			}

			return l.(int) - r.(int), nil
		})

	expr.Define(alternation.New[rune, runes.Pos](difference, number()))

	root := concatenation.New[rune, runes.Pos](expr, end.New[rune, runes.Pos]())

	for _, in := range []string{"7", "10-3", "10-3-2", "100-1-2-3-4"} {
		rd := input(in)

		ok, m, err := ebnf.Parse[rune, runes.Pos](root, rd)
		if err != nil || !ok {
			fmt.Println(in, "does not match", err)
			continue
		}

		v, _ := m.Components[0].Eval(rd)
		fmt.Printf("%s = %v\n", in, v)
	}

	// Output:
	// 7 = 7
	// 10-3 = 7
	// 10-3-2 = 5
	// 100-1-2-3-4 = 90
}

// A rule that calls itself at the same position without being marked is reported, naming the rule, and does not
// overflow the stack.
func ExampleErrLeftRecursion() {
	expr := rule.Declare[rune, runes.Pos]("expr")
	expr.Define(alternation.New[rune, runes.Pos](concatenation.New[rune, runes.Pos](expr, char('-'), number()), number()))

	_, _, err := ebnf.Parse[rune, runes.Pos](expr, input("1-2"))
	fmt.Println(err)

	// Output: exbana: left recursion: rule "expr" calls itself at the same position, mark it with SetLeftRecursive
}
