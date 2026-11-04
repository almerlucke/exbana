// Package rule provides Rule, a named grammar rule: a pattern with a name, an optional evaluation function and
// support for forward declarations (needed for recursive grammars).
package rule

import (
	"errors"
	"fmt"
	"io"
	"slices"

	ebnf "github.com/almerlucke/exbana/v3"
)

// Rule gives a pattern a name and an evaluation function. It is the unit a grammar is made of:
//
//	number := rule.New("number", digits).SetEval(evalNumber)
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
	leftRec  bool
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

// SetEval sets the function that evaluates a match of this rule.
func (r *Rule[T, P]) SetEval(f func(*ebnf.Match[T, P], ebnf.Reader[T, P]) (any, error)) *Rule[T, P] {
	r.evalFunc = f
	return r
}

// SetAtomic makes the rule report failure as a whole: syntax errors name the rule ("expected number") instead
// of the leaf patterns inside it ("expected digit or '-'"). It is meant for token-like rules.
func (r *Rule[T, P]) SetAtomic(atomic bool) *Rule[T, P] {
	r.atomic = atomic
	return r
}

// SetLeftRecursive allows the rule to be left recursive, that is to start with a call to itself at the same position:
//
//	expr = expr "-" number | number
//
// Such a rule can't be matched by trying its definition and recursing, it would never end. Instead the rule works out
// its matches by growing a seed: first without the recursive alternatives, then again with the matches found so far
// handed to the recursive calls, until no new match appears. The result is left associative, "1-2-3" matches as
// "(1-2)-3". Indirect left recursion (a = b "x", b = a "y") works if one rule in the cycle is marked.
//
// A marked rule works out all its matches before it offers the first, and offers them longest first. The work is
// proportional to the number of matches, a chain of n terms takes time linear in n. What a marked rule found at a
// position is remembered for the rest of the parse, so using it at many positions, like an expression inside the
// parentheses of an expression, costs no more than parsing the text once.
//
// A rule that is not marked and is entered again at the same position returns ErrLeftRecursion instead of
// recursing without end.
func (r *Rule[T, P]) SetLeftRecursive(leftRecursive bool) *Rule[T, P] {
	r.leftRec = leftRecursive
	return r
}

// ErrLeftRecursion is returned when a rule is entered again at the same position and is not marked with SetLeftRecursive.
var ErrLeftRecursion = errors.New("exbana: left recursion")

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

	ctx := ebnf.EnsureContext(rd)

	begin, err := ctx.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	// entered again at the position where it was entered, without consuming anything: left recursion
	if open := ctx.FindOpenFrame(ebnf.Pattern[T, P](r), begin); open != nil {
		if !open.Grows {
			return false, fmt.Errorf("%w: rule %q calls itself at the same position, mark it with SetLeftRecursive", ErrLeftRecursion, r.ID())
		}

		open.Used = true

		return offer(ctx, open.Seed, k)
	}

	if r.leftRec {
		return r.matchGrowingCached(ctx, begin, k)
	}

	// rules nested ten thousands of levels deep, like the parentheses in ((((...)))), use up the stack
	if err := ebnf.CheckStack(ctx.FrameDepth()); err != nil {
		return false, err
	}

	frame := ctx.PushFrame(r, begin, false)
	defer ctx.PopFrame(frame)

	inner := ebnf.Reader[T, P](ctx)
	if r.atomic {
		inner = ebnf.Silent(inner)
	}

	matched, err := r.inner.MatchCont(inner, func(m *ebnf.Match[T, P]) (bool, error) {
		// the rule has matched, the continuation is not part of working out the match
		frame.Open = false
		matched, err := k(r.wrap(m))
		frame.Open = true

		return matched, err
	})
	if err != nil || matched {
		return matched, err
	}

	r.logFailure(ctx, begin)

	return false, nil
}

func (r *Rule[T, P]) wrap(m *ebnf.Match[T, P]) *ebnf.Match[T, P] {
	return &ebnf.Match[T, P]{Pattern: r, Begin: m.Begin, End: m.End, Value: m.Value, Components: m.Components}
}

func (r *Rule[T, P]) logFailure(rd ebnf.Reader[T, P], begin P) {
	if r.atomic {
		ebnf.LogFailure(rd, ebnf.Pattern[T, P](r), begin, begin)
	}
}

// offer offers the matches to the continuation in order, until one is accepted.
func offer[T, P any](rd ebnf.Reader[T, P], matches []*ebnf.Match[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	for _, m := range matches {
		if err := rd.SetPosition(m.End); ebnf.IsStreamError(err) {
			return false, err
		}

		if matched, err := k(m); err != nil || matched {
			return matched, err
		}
	}

	return false, nil
}

// matchGrowingCached is matchGrowing, but a rule that is used at the same position again gets the matches it found
// the first time.
func (r *Rule[T, P]) matchGrowingCached(ctx *ebnf.ParseContext[T, P], begin P, k ebnf.Continuation[T, P]) (bool, error) {
	if results, known := ctx.RuleResult(r, begin); known {
		return offer(ctx, results, k)
	}

	return r.matchGrowing(ctx, begin, k)
}

// matchGrowing matches a left recursive rule by growing a seed until it is stable.
//
// Each round runs the definition with the left recursive calls answered from the seed, and what it finds that is not
// known yet joins the matches. The next round only has to answer the recursive calls with the new matches, which
// makes the work proportional to the number of matches and not to its square. That is enough because a match built
// on an old match was found in an earlier round, and a second recursive call at the same position can only follow an
// empty match, so what it combines ends where the other seed match ends: it adds a different tree for an end that
// is already known, and ends are what matching needs.
//
// The matches are offered longest first.
func (r *Rule[T, P]) matchGrowing(ctx *ebnf.ParseContext[T, P], begin P, k ebnf.Continuation[T, P]) (bool, error) {
	frame := ctx.PushFrame(r, begin, true)

	inner := ebnf.Reader[T, P](ctx)
	if r.atomic {
		inner = ebnf.Silent(inner)
	}

	var (
		results []*ebnf.Match[T, P]
		known   = ebnf.NewEndSet[T, P](ctx)
	)

	for {
		frame.Used = false

		// every round starts at the beginning, the previous one left the reader wherever it stopped
		if err := ctx.SetPosition(begin); ebnf.IsStreamError(err) {
			ctx.PopFrame(frame)
			return false, err
		}

		var fresh []*ebnf.Match[T, P]

		_, err := r.inner.MatchCont(inner, func(m *ebnf.Match[T, P]) (bool, error) {
			if known.Add(m.End) {
				fresh = append(fresh, r.wrap(m))
			}

			return false, nil
		})
		if err != nil {
			ctx.PopFrame(frame)
			return false, err
		}

		results = append(results, fresh...)

		// nothing used the seed, or nothing new was found: the seed is stable
		if !frame.Used || len(fresh) == 0 {
			break
		}

		frame.Seed = fresh
	}

	// the rule is done, what follows is not part of working out its matches
	ctx.PopFrame(frame)

	slices.SortStableFunc(results, func(a, b *ebnf.Match[T, P]) int {
		return ctx.Length(begin, b.End) - ctx.Length(begin, a.End)
	})

	ctx.SetRuleResult(r, begin, results)

	if len(results) == 0 {
		r.logFailure(ctx, begin)
		return false, nil
	}

	return offer(ctx, results, k)
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

// Shape implements ebnf.Structured. The part of a rule is its definition, a rule that is declared and not defined has none.
func (r *Rule[T, P]) Shape() ebnf.Shape[T, P] {
	s := ebnf.Shape[T, P]{Kind: ebnf.KindRule, ID: r.ID(), LeftRecursive: r.leftRec, Atomic: r.atomic}
	if r.inner != nil {
		s.Parts = ebnf.Patterns[T, P]{r.inner}
	}

	return s
}
