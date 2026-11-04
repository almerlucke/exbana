// Package rule provides Rule, a named grammar rule: a pattern with a name, an optional evaluation function and
// support for forward declarations (needed for recursive grammars).
package rule

import (
	"fmt"
	"io"

	ebnf "github.com/almerlucke/exbana/v2"
)

// Rule gives a pattern a name and an evaluation function. It is the unit a grammar is made of:
//
//	number := rule.New("number", digits).WithEval(evalNumber)
//
// The match of a rule has the rule as its Pattern (so ID and Eval are the rule's) and the Value and Components
// of the pattern it wraps, so an eval function sees the same shape the wrapped pattern produced.
//
// A rule can be declared before it is defined, which is how recursion is written:
//
//	expr := rule.Declare[rune, runes.Pos]("expr")
//	paren := conc('(', expr, ')')
//	expr.Define(alt(number, paren))
//
// Define must be called before the rule is used to match or generate, matching an undefined rule is an error.
type Rule[T, P any] struct {
	ebnf.Label
	inner    ebnf.Pattern[T, P]
	atomic   bool
	evalFunc func(*ebnf.Match[T, P], ebnf.Reader[T, P]) (any, error)
}

// New creates a rule with a name and a definition.
func New[T, P any](name string, inner ebnf.Pattern[T, P]) *Rule[T, P] {
	r := &Rule[T, P]{inner: inner}
	r.SetID(name)
	return r
}

// Declare creates a rule without a definition, to be defined later with Define.
func Declare[T, P any](name string) *Rule[T, P] {
	return New[T, P](name, nil)
}

// Define sets the pattern the rule stands for.
func (r *Rule[T, P]) Define(inner ebnf.Pattern[T, P]) *Rule[T, P] {
	r.inner = inner
	return r
}

// Defined returns true if the rule has a definition.
func (r *Rule[T, P]) Defined() bool {
	return r.inner != nil
}

// WithEval sets the function that evaluates a match of this rule.
func (r *Rule[T, P]) WithEval(f func(*ebnf.Match[T, P], ebnf.Reader[T, P]) (any, error)) *Rule[T, P] {
	r.evalFunc = f
	return r
}

// SetAtomic makes the rule report failure as a whole: syntax errors name the rule ("expected number") instead
// of the leaf patterns inside it ("expected digit or '-'"). It is meant for token-like rules.
func (r *Rule[T, P]) SetAtomic(atomic bool) *Rule[T, P] {
	r.atomic = atomic
	return r
}

// Eval evaluates a match of this rule: with the eval function if there is one, otherwise with the wrapped pattern
// if that can evaluate, otherwise it returns the matched value.
func (r *Rule[T, P]) Eval(m *ebnf.Match[T, P], rd ebnf.Reader[T, P]) (any, error) {
	if r.evalFunc != nil {
		return r.evalFunc(m, rd)
	}

	if e, ok := r.inner.(ebnf.Evaluator[T, P]); ok {
		return e.Eval(m, rd)
	}

	return m.Value, nil
}

func (r *Rule[T, P]) undefined() error {
	return fmt.Errorf("rule %q is declared but not defined", r.ID())
}

// Match implements Pattern
func (r *Rule[T, P]) Match(rd ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	return ebnf.FirstMatch(rd, r.MatchCont)
}

// MatchCont offers every match of the wrapped pattern, as a match of the rule.
func (r *Rule[T, P]) MatchCont(rd ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	if r.inner == nil {
		return false, r.undefined()
	}

	inner := rd
	if r.atomic {
		inner = ebnf.Silent(rd)
	}

	begin, err := rd.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	matched, err := r.inner.MatchCont(inner, func(m *ebnf.Match[T, P]) (bool, error) {
		return k(&ebnf.Match[T, P]{Pattern: r, Begin: m.Begin, End: m.End, Value: m.Value, Components: m.Components})
	})
	if err != nil || matched {
		return matched, err
	}

	if r.atomic {
		ebnf.LogMismatch(rd, ebnf.Pattern[T, P](r), func() *ebnf.Mismatch[T, P] {
			return ebnf.NewMismatch(r, begin, begin, nil, nil)
		})
	}

	return false, nil
}

// Generate generates what the wrapped pattern generates.
func (r *Rule[T, P]) Generate(ctx *ebnf.GenContext, w ebnf.Writer[T]) error {
	if r.inner == nil {
		return r.undefined()
	}

	if err := ctx.Enter(); err != nil {
		return err
	}
	defer ctx.Leave()

	return r.inner.Generate(ctx, w)
}

// Print prints the definition of the rule. Inside other patterns a rule prints as its name, and ebnf.PrintRules
// prints "name = definition".
func (r *Rule[T, P]) Print(w io.Writer) error {
	if r.inner == nil {
		_, err := io.WriteString(w, "?")
		return err
	}

	return r.inner.Print(w)
}

// Children returns the wrapped pattern
func (r *Rule[T, P]) Children() ebnf.Patterns[T, P] {
	if r.inner == nil {
		return nil
	}

	return ebnf.Patterns[T, P]{r.inner}
}
