// Command expr is an arithmetic expression calculator built with exbana. It is a test of how well the library
// handles a real expression grammar: precedence, associativity, unary operators, function calls and syntax errors.
//
// There is no left recursion in the grammar, as exbana can't do that. Precedence is encoded as levels
// (expr > term > unary > power > primary), left associativity is done by folding a repetition when evaluating,
// and right associativity (^) by recursing on the right.
//
//	expr    = term   , { ("+" | "-")       , term }
//	term    = unary  , { ("*" | "/" | "%") , unary }
//	unary   = ("-" | "+") , unary | power
//	power   = primary , [ "^" , unary ]
//	primary = number | call | variable | "(" , expr , ")"
//	call    = identifier , "(" , [ expr , { "," , expr } ] , ")"
//
// Whitespace is allowed between all tokens.
package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v2"
	"github.com/almerlucke/exbana/v2/patterns/alternation"
	"github.com/almerlucke/exbana/v2/patterns/concatenation"
	"github.com/almerlucke/exbana/v2/patterns/end"
	ent "github.com/almerlucke/exbana/v2/patterns/entity"
	"github.com/almerlucke/exbana/v2/patterns/repetition"
	"github.com/almerlucke/exbana/v2/patterns/rule"
	"github.com/almerlucke/exbana/v2/readers/runes"
)

type (
	pattern = ebnf.Pattern[rune, runes.Pos]
	match   = ebnf.Match[rune, runes.Pos]
	reader  = ebnf.Reader[rune, runes.Pos]
)

// Mode selects how the grammar is built
type Mode int

const (
	// Backtracking uses the default semantics: greedy repetitions that give back, longest-first alternations.
	Backtracking Mode = iota
	// Backtracking with failure memoization on the reader.
	Memoized
	// Committed uses possessive repetitions and orthogonal alternations, so nothing backtracks.
	Committed
)

func (m Mode) String() string {
	return [...]string{"backtracking", "memoized", "committed"}[m]
}

var functions = map[string]func(args []float64) (float64, error){
	"sqrt":  unary1(math.Sqrt),
	"abs":   unary1(math.Abs),
	"sin":   unary1(math.Sin),
	"cos":   unary1(math.Cos),
	"floor": unary1(math.Floor),
	"max": func(args []float64) (float64, error) {
		if len(args) == 0 {
			return 0, fmt.Errorf("max needs at least one argument")
		}
		m := args[0]
		for _, a := range args[1:] {
			m = math.Max(m, a)
		}
		return m, nil
	},
	"min": func(args []float64) (float64, error) {
		if len(args) == 0 {
			return 0, fmt.Errorf("min needs at least one argument")
		}
		m := args[0]
		for _, a := range args[1:] {
			m = math.Min(m, a)
		}
		return m, nil
	},
}

func unary1(f func(float64) float64) func([]float64) (float64, error) {
	return func(args []float64) (float64, error) {
		if len(args) != 1 {
			return 0, fmt.Errorf("expected 1 argument, got %d", len(args))
		}
		return f(args[0]), nil
	}
}

// Calculator evaluates arithmetic expressions. Eval can be called from several goroutines at once, as long as Vars
// is not changed meanwhile.
type Calculator struct {
	Vars map[string]float64

	mode Mode
	root pattern
}

// NewCalculator builds the grammar for a mode.
func NewCalculator(mode Mode) *Calculator {
	c := &Calculator{Vars: map[string]float64{}, mode: mode}
	c.root = c.grammar()
	return c
}

// Eval parses and evaluates an expression. Syntax errors report the farthest position that was reached.
func (c *Calculator) Eval(input string) (float64, error) {
	rd, err := runes.New(strings.NewReader(input))
	if err != nil {
		return 0, err
	}

	// everything specific to this parse lives in the context, the grammar is never changed. That makes Eval safe to
	// call from several goroutines at once.
	log := runes.NewFarthestLog(rd)
	ctx := ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log)
	if c.mode == Memoized {
		ctx.WithMemo(func(p runes.Pos) int { return p.Index })
	}
	var r reader = ctx

	matched, m, err := ebnf.Parse(c.root, r)
	if err != nil {
		return 0, err
	}
	if !matched {
		if err := log.Err(); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("syntax error")
	}

	return num(m, r)
}

func (c *Calculator) grammar() pattern {
	fast := c.mode == Committed

	rep := func(p pattern, min, max int) *repetition.Repetition[rune, runes.Pos] {
		return repetition.New(p, min, max).SetPossessive(fast)
	}
	any0 := func(p pattern) *repetition.Repetition[rune, runes.Pos] { return rep(p, 0, 0) }
	opt := func(p pattern) *repetition.Repetition[rune, runes.Pos] { return rep(p, 0, 1) }
	more1 := func(p pattern) *repetition.Repetition[rune, runes.Pos] { return rep(p, 1, 0) }
	conc := func(ps ...pattern) *concatenation.Concatenation[rune, runes.Pos] { return concatenation.New(ps...) }
	alt := func(ps ...pattern) *alternation.Alternation[rune, runes.Pos] {
		return alternation.New(ps...).SetOrthogonal(fast)
	}
	char := func(r rune) pattern {
		return ebnf.Text(ent.New[rune, runes.Pos](func(x rune) bool { return x == r }), strconv.Quote(string(r)))
	}
	oneOf := func(chars string, name string) pattern {
		return ebnf.Name(ent.New[rune, runes.Pos](func(x rune) bool { return strings.ContainsRune(chars, x) }), name)
	}
	class := func(f func(rune) bool, name string) pattern {
		return ebnf.Name(ent.New[rune, runes.Pos](f), name)
	}

	ws := ebnf.Name(any0(ent.New[rune, runes.Pos](unicode.IsSpace)), "ws")
	digit := func() pattern { return class(func(r rune) bool { return r >= '0' && r <= '9' }, "digit") }
	letter := func() pattern { return class(func(r rune) bool { return unicode.IsLetter(r) || r == '_' }, "letter") }

	number := rule.New("number", conc(
		more1(digit()),
		opt(conc(char('.'), more1(digit()))),
		opt(conc(oneOf("eE", "exponent"), opt(oneOf("+-", "sign")), more1(digit()))),
	)).WithEval(evalNumber)

	identifier := func() pattern {
		return ebnf.Name(conc(letter(), any0(alt(letter(), digit()))), "identifier")
	}

	variable := rule.New("variable", identifier()).WithEval(c.evalVariable)

	// forward declarations for the recursive rules
	expr := rule.Declare[rune, runes.Pos]("expression").WithEval(evalBinary)
	unary := rule.Declare[rune, runes.Pos]("unary").WithEval(delegate)

	args := opt(conc(expr, any0(conc(ws, char(','), ws, expr))))
	call := rule.New("call", conc(identifier(), ws, char('('), ws, args, ws, char(')'))).WithEval(evalCall)

	paren := rule.New("parenthesis", conc(char('('), ws, expr, ws, char(')'))).WithEval(func(m *match, r reader) (any, error) {
		return num(m.Components[2], r)
	})

	primary := rule.New("primary", alt(number, call, variable, paren)).WithEval(delegate)

	power := rule.New("power", conc(primary, opt(conc(ws, char('^'), ws, unary)))).WithEval(evalPower)

	negation := rule.New("negation", conc(oneOf("+-", "sign"), ws, unary)).WithEval(evalNegation)

	term := rule.New("term", conc(unary, any0(conc(ws, oneOf("*/%", "multiplication operator"), ws, unary)))).WithEval(evalBinary)

	expr.Define(conc(term, any0(conc(ws, oneOf("+-", "addition operator"), ws, term))))
	unary.Define(alt(negation, power))

	return rule.New("input", conc(ws, expr, ws, ebnf.Text(end.New[rune, runes.Pos](), "end of input"))).WithEval(func(m *match, r reader) (any, error) {
		return num(m.Components[1], r)
	})
}

// num evaluates a match that is known to give a float
func num(m *match, r reader) (float64, error) {
	v, err := m.Eval(r)
	if err != nil {
		return 0, err
	}
	return v.(float64), nil
}

func text(m *match, r reader) (string, error) {
	rs, err := r.Range(m.Begin, m.End)
	return string(rs), err
}

// delegate evaluates the one alternative that matched
func delegate(m *match, r reader) (any, error) {
	return m.Components[0].Eval(r)
}

func evalNumber(m *match, r reader) (any, error) {
	s, err := text(m, r)
	if err != nil {
		return nil, err
	}
	return strconv.ParseFloat(s, 64)
}

func (c *Calculator) evalVariable(m *match, r reader) (any, error) {
	name, err := text(m, r)
	if err != nil {
		return nil, err
	}
	v, ok := c.Vars[name]
	if !ok {
		return nil, fmt.Errorf("undefined variable %q", name)
	}
	return v, nil
}

func evalCall(m *match, r reader) (any, error) {
	name, err := text(m.Components[0], r)
	if err != nil {
		return nil, err
	}
	f, ok := functions[name]
	if !ok {
		return nil, fmt.Errorf("undefined function %q", name)
	}

	var args []float64
	if list := m.Components[4].Components; len(list) == 1 {
		first, err := num(list[0].Components[0], r)
		if err != nil {
			return nil, err
		}
		args = append(args, first)
		for _, more := range list[0].Components[1].Components {
			a, err := num(more.Components[3], r)
			if err != nil {
				return nil, err
			}
			args = append(args, a)
		}
	}

	return f(args)
}

// evalBinary folds `operand { operator operand }` from the left, which makes the operators left associative
func evalBinary(m *match, r reader) (any, error) {
	acc, err := num(m.Components[0], r)
	if err != nil {
		return nil, err
	}

	for _, item := range m.Components[1].Components {
		op, err := text(item.Components[1], r)
		if err != nil {
			return nil, err
		}
		right, err := num(item.Components[3], r)
		if err != nil {
			return nil, err
		}
		switch op {
		case "+":
			acc += right
		case "-":
			acc -= right
		case "*":
			acc *= right
		case "/":
			acc /= right
		case "%":
			acc = math.Mod(acc, right)
		}
	}

	return acc, nil
}

func evalPower(m *match, r reader) (any, error) {
	base, err := num(m.Components[0], r)
	if err != nil {
		return nil, err
	}
	if rest := m.Components[1].Components; len(rest) == 1 {
		exp, err := num(rest[0].Components[3], r)
		if err != nil {
			return nil, err
		}
		return math.Pow(base, exp), nil
	}
	return base, nil
}

func evalNegation(m *match, r reader) (any, error) {
	sign, err := text(m.Components[0], r)
	if err != nil {
		return nil, err
	}
	v, err := num(m.Components[2], r)
	if err != nil {
		return nil, err
	}
	if sign == "-" {
		return -v, nil
	}
	return v, nil
}

func main() {
	mode := Committed
	calc := NewCalculator(mode)
	calc.Vars["x"] = 5
	calc.Vars["pi"] = math.Pi

	inputs := os.Args[1:]
	if len(inputs) == 0 {
		inputs = []string{
			"1 + 2 * 3",
			"(1 + 2) * 3",
			"2 ^ 3 ^ 2",
			"-2 ^ 2",
			"10 - 4 - 3",
			"max(1, x * 2, 3) + sqrt(16)",
			"2 * pi * x",
			"1 +",
			"1 + * 2",
			"(1 + 2",
			"foo(1)",
		}
	}

	for _, in := range inputs {
		v, err := calc.Eval(in)
		if err != nil {
			fmt.Printf("%-30s error: %v\n", in, err)
			continue
		}
		fmt.Printf("%-30s = %v\n", in, v)
	}
}
