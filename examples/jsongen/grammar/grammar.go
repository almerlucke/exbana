// Package grammar is JSON as a grammar of patterns, and the generated parser for it. The grammar is written the way it
// reads, with the default backtracking repetitions and alternations; the optimizer takes the backtracking out, and the code
// generator turns the result into a parser (see the parser package) that does not use this library.
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

func oneOf(rs ...rune) pattern { return entity.OneOf[rune, runes.Pos](rs...) }

// JSON returns the grammar. Every call makes a new one, because optimizing changes the patterns.
//
//	value  = string | number | array | object | literal
//	array  = "[" [ value { "," value } ] "]"
//	object = "{" [ member { "," member } ] "}"
//
// Whitespace is part of the token before it, so that every choice can be made by looking at the next character.
func JSON() pattern {
	digit := entity.Between[rune, runes.Pos]('0', '9')

	ws := many(oneOf(' ', '\t', '\n', '\r'))

	integer := alt(text("0"), conc(entity.Between[rune, runes.Pos]('1', '9'), many(digit)))
	fraction := conc(text("."), some(digit))
	exponent := conc(oneOf('e', 'E'), maybe(oneOf('+', '-')), some(digit))
	number := rule.New[rune, runes.Pos]("number", conc(maybe(text("-")), integer, maybe(fraction), maybe(exponent), ws))

	escape := conc(text(`\`), alt(oneOf('"', '\\', '/', 'b', 'f', 'n', 'r', 't'), conc(text("u"), digit, digit, digit, digit)))
	str := rule.New[rune, runes.Pos]("string", conc(text(`"`),
		many(alt(entity.NotIn[rune, runes.Pos](entity.Range[rune]{Lo: '"', Hi: '"'}, entity.Range[rune]{Lo: '\\', Hi: '\\'}), escape)),
		text(`"`), ws))

	literal := rule.New[rune, runes.Pos]("literal", conc(alt(text("true"), text("false"), text("null")), ws))

	value := rule.Declare[rune, runes.Pos]("value")

	comma := conc(text(","), ws)
	array := rule.New[rune, runes.Pos]("array", conc(text("["), ws, maybe(conc(value, many(conc(comma, value)))), text("]"), ws))

	member := rule.New[rune, runes.Pos]("member", conc(str, text(":"), ws, value))
	object := rule.New[rune, runes.Pos]("object", conc(text("{"), ws, maybe(conc(member, many(conc(comma, member)))), text("}"), ws))

	value.Define(alt(str, number, array, object, literal))

	return conc(ws, value, end.New[rune, runes.Pos]().Printed("end of input"))
}

// Source returns the source of the parser for the grammar, in package parser. The generator (see gen) writes it to
// parser/parser.go, and the tests check that file is what this returns.
func Source() ([]byte, error) {
	g := JSON()

	res, err := optimize.Optimize[rune, runes.Pos](g, optimize.Options{})
	if err != nil {
		return nil, err
	}

	// the parser can only be generated for a grammar that does not backtrack: say what is in the way if it ever does
	if blocked := res.Blocked(); len(blocked) > 0 {
		return nil, fmt.Errorf("the grammar backtracks in %d places:\n%v", len(blocked), res)
	}

	return codegen.Generate[rune, runes.Pos](g, codegen.Config[rune, runes.Pos]{Package: "parser", Name: "JSON"})
}
