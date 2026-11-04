// Package grammar is arithmetic as a grammar of patterns, written with left recursive rules the way it reads, and the code
// generator turns them into loops. The operators group to the left, "10-3-2" is "(10-3)-2".
package grammar

import (
	"fmt"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/codegen"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

type pattern = ebnf.Pattern[rune, runes.Pos]

func conc(ps ...pattern) pattern { return concatenation.New[rune, runes.Pos](ps...) }
func alt(ps ...pattern) pattern  { return alternation.New[rune, runes.Pos](ps...) }
func text(s string) pattern      { return vector.Text[runes.Pos](s) }
func many(p pattern) pattern     { return repetition.Any[rune, runes.Pos](p) }
func some(p pattern) pattern     { return repetition.OneOrMore[rune, runes.Pos](p) }
func maybe(p pattern) pattern    { return repetition.Optional[rune, runes.Pos](p) }

// Calc returns the grammar. Every call makes a new one, because optimizing changes the patterns.
//
//	input   = expr end
//	expr    = expr addop term | term               left recursive
//	term    = term mulop unary | unary             left recursive
//	unary   = "-" unary | primary
//	primary = number | "(" expr ")"
//
// White space is part of the token before it, so that every choice can be made by looking at the next character.
func Calc() pattern {
	digit := entity.Between[rune, runes.Pos]('0', '9').Printed("digit")
	ws := many(entity.OneOf[rune, runes.Pos](' ', '\t', '\n'))

	number := rule.New[rune, runes.Pos]("number", conc(some(digit), maybe(conc(text("."), some(digit))), ws))
	addop := rule.New[rune, runes.Pos]("addop", conc(alt(text("+"), text("-")), ws))
	mulop := rule.New[rune, runes.Pos]("mulop", conc(alt(text("*"), text("/"), text("%")), ws))

	expr := rule.Declare[rune, runes.Pos]("expr").SetLeftRecursive(true)
	term := rule.Declare[rune, runes.Pos]("term").SetLeftRecursive(true)
	unary := rule.Declare[rune, runes.Pos]("unary")
	primary := rule.Declare[rune, runes.Pos]("primary")

	expr.Define(alt(conc(expr, addop, term), term))
	term.Define(alt(conc(term, mulop, unary), unary))
	unary.Define(alt(conc(text("-"), ws, unary), primary))
	primary.Define(alt(number, conc(text("("), ws, expr, text(")"), ws)))

	return rule.New[rune, runes.Pos]("input", conc(ws, expr, end.New[rune, runes.Pos]().Printed("end of input")))
}

// Source returns the source of the parser for the grammar, in package parser.
func Source() ([]byte, error) {
	g := Calc()

	res, err := optimize.Optimize[rune, runes.Pos](g, optimize.Options{Closed: true})
	if err != nil {
		return nil, err
	}

	if blocked := res.Blocked(); len(blocked) > 0 {
		return nil, fmt.Errorf("the grammar backtracks in %d places:\n%v", len(blocked), res)
	}

	return codegen.Generate[rune, runes.Pos](g, codegen.Config[rune, runes.Pos]{Package: "parser", Name: "Calc", UTF8: true})
}
