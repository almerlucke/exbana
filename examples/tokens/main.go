// Command tokens is a calculator in two stages: a small lexer turns the text into tokens, and the grammar parses the
// slice of tokens through a slice reader. The grammar is the usual one for arithmetic,
//
//	expression = expression ("+" | "-") term | term
//	term       = term ("*" | "/") factor | factor
//	factor     = number | "(" expression ")"
//
// written the way it reads: the two left recursive rules make the operators left associative, "10-3-2" is "(10-3)-2".
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/slice"
)

// Kind is the kind of a token
type Kind int

const (
	Number Kind = iota
	Plus
	Minus
	Star
	Slash
	LParen
	RParen
)

// Token is what the lexer produces and the grammar matches
type Token struct {
	Kind  Kind
	Text  string
	Value float64 // for numbers
	Pos   int     // where in the text the token starts
}

// Lex turns text into tokens.
func Lex(input string) ([]Token, error) {
	var tokens []Token

	runes := []rune(input)

	for i := 0; i < len(runes); {
		switch c := runes[i]; {
		case unicode.IsSpace(c):
			i++
		case unicode.IsDigit(c) || c == '.':
			start := i
			for i < len(runes) && (unicode.IsDigit(runes[i]) || runes[i] == '.') {
				i++
			}

			text := string(runes[start:i])

			v, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, fmt.Errorf("bad number %q at %d", text, start)
			}

			tokens = append(tokens, Token{Kind: Number, Text: text, Value: v, Pos: start})
		case strings.ContainsRune("+-*/()", c):
			kind := map[rune]Kind{'+': Plus, '-': Minus, '*': Star, '/': Slash, '(': LParen, ')': RParen}[c]
			tokens = append(tokens, Token{Kind: kind, Text: string(c), Pos: i})
			i++
		default:
			return nil, fmt.Errorf("unexpected %q at %d", c, i)
		}
	}

	return tokens, nil
}

type (
	pattern = ebnf.Pattern[Token, int]
	match   = ebnf.Match[Token, int]
	reader  = ebnf.Reader[Token, int]
)

// Calculator holds the grammar, which is built once and can be used by any number of goroutines.
type Calculator struct {
	root pattern
}

// NewCalculator builds the grammar.
func NewCalculator() *Calculator {
	kind := func(k Kind, text string) *ent.Entity[Token, int] {
		return ent.New[Token, int](func(t Token) bool { return t.Kind == k }).Printed(text)
	}
	conc := concatenation.New[Token, int]
	alt := alternation.New[Token, int]

	number := rule.New[Token, int]("number", kind(Number, "number")).SetEval(func(m *match, r reader) (any, error) {
		tokens, err := r.Range(m.Begin, m.End)
		return tokens[0].Value, err
	})

	// both operator rules are the same shape: left operand, operator, right operand
	binary := func(name string, left, operator, right pattern, apply func(op Kind, l, r float64) (float64, error)) *rule.Rule[Token, int] {
		return rule.New(name, conc(left, operator, right)).SetEval(func(m *match, r reader) (any, error) {
			l, err := m.Components[0].Eval(r)
			if err != nil {
				return nil, err
			}

			v, err := m.Components[2].Eval(r)
			if err != nil {
				return nil, err
			}

			op, err := r.Range(m.Components[1].Begin, m.Components[1].End)
			if err != nil {
				return nil, err
			}

			return apply(op[0].Kind, l.(float64), v.(float64))
		})
	}

	// a rule that stands for one of its alternatives: its value is the value of the one that matched
	delegate := func(m *match, r reader) (any, error) { return m.Components[0].Eval(r) }

	expression := rule.Declare[Token, int]("expression").SetLeftRecursive(true).SetEval(delegate)
	term := rule.Declare[Token, int]("term").SetLeftRecursive(true).SetEval(delegate)
	factor := rule.Declare[Token, int]("factor").SetEval(delegate)

	sum := binary("sum", expression, alt(kind(Plus, `"+"`), kind(Minus, `"-"`)), term,
		func(op Kind, l, r float64) (float64, error) {
			if op == Plus {
				return l + r, nil
			}
			return l - r, nil
		})

	product := binary("product", term, alt(kind(Star, `"*"`), kind(Slash, `"/"`)), factor,
		func(op Kind, l, r float64) (float64, error) {
			if op == Star {
				return l * r, nil
			}
			if r == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return l / r, nil
		})

	parenthesis := rule.New("parenthesis", conc(kind(LParen, `"("`), expression, kind(RParen, `")"`))).
		SetEval(func(m *match, r reader) (any, error) { return m.Components[1].Eval(r) })

	expression.Define(alt(sum, term))
	term.Define(alt(product, factor))
	factor.Define(alt(number, parenthesis))

	root := rule.New("input", conc(expression, end.New[Token, int]().Printed("end of input"))).
		SetEval(func(m *match, r reader) (any, error) { return m.Components[0].Eval(r) })

	return &Calculator{root: root}
}

// Eval lexes and parses the input and returns its value.
func (c *Calculator) Eval(input string) (float64, error) {
	tokens, err := Lex(input)
	if err != nil {
		return 0, err
	}

	rd := slice.New(tokens)
	log := slice.NewFarthestLog(rd, func(t Token) string { return strconv.Quote(t.Text) })

	ok, m, err := ebnf.Parse[Token, int](c.root, ebnf.NewParseContext[Token, int](rd).WithLogger(log))
	if err != nil {
		return 0, err
	}

	if !ok {
		// say where in the text the failure is, not just which token it is
		if e := log.Last(); e != nil && !e.AtEnd {
			return 0, fmt.Errorf("%w (column %d)", e, tokens[e.Pos].Pos+1)
		}

		return 0, log.Err()
	}

	v, err := m.Eval(rd)
	if err != nil {
		return 0, err
	}

	return v.(float64), nil
}

func main() {
	calc := NewCalculator()

	for _, input := range os.Args[1:] {
		v, err := calc.Eval(input)
		if err != nil {
			fmt.Printf("%s: %v\n", input, err)
			continue
		}

		fmt.Printf("%s = %v\n", input, v)
	}

	if len(os.Args) == 1 {
		for _, input := range []string{"1 + 2 * 3", "10 - 3 - 2", "(1 + 2) * 3", "8 / 4 / 2", "2 * (3 + ", "4 4"} {
			v, err := calc.Eval(input)
			if err != nil {
				fmt.Printf("%s: %v\n", input, err)
				continue
			}

			fmt.Printf("%s = %v\n", input, v)
		}
	}
}
