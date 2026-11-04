package optimize_test

import (
	"fmt"
	"strings"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// Optimize finds the repetitions that never need to give anything back and the alternations whose branches start with
// different elements, and changes them, so that the grammar matches exactly what it did but without the backtracking. The
// report says what was found, and for what was not changed, why not.
func ExampleOptimize() {
	digits := some(between('0', '9'))
	sign := alt(text("+"), text("-"))

	// number = [ sign ] digits [ "." digits ] ";"
	number := conc(maybe(sign), digits, maybe(conc(text("."), some(between('0', '9')))), text(";"))

	// and a trailing a* that is followed by an a: this one needs its backtracking
	tricky := conc(many(text("a")), text("a"))

	grammar := alt(number, tricky)

	result, err := optimize.Optimize[rune, runes.Pos](grammar, optimize.Options{})
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Print(result)

	rd, _ := runes.New(strings.NewReader("-12.5;"))
	ok, m, _ := ebnf.Parse[rune, runes.Pos](grammar, rd)
	fmt.Println(ok, m.End.Index)

	// Output:
	// 5 can be made possessive or orthogonal, 2 can not
	//   safe possessive: ("+" | "-")?: the item starts with '+', '-', what follows never does
	//   safe orthogonal: ("+" | "-"): the branches start with different elements, so at most one can match
	//   safe possessive: [0-9]+: the item starts with '0' to '9', what follows never does
	//   safe possessive: (".", [0-9]+)?: the item starts with '.', what follows never does
	//   safe possessive: [0-9]+: the item starts with '0' to '9', what follows never does
	//   not safe orthogonal: ((("+" | "-")?, [0-9]+, (".", [0-9]+)?, ";") | ("a"*, "a")): branch 2 can match in more than one way
	//   not safe possessive: "a"*: the repeated item and what follows it can both start with 'a'
	// true 6
}
