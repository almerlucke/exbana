// Package optimize makes a grammar faster without changing what it matches. A grammar made of patterns backtracks: a
// repetition gives back what it matched when what follows fails, and an alternation tries its other branches. That is
// what makes it easy to write and slow to run. Most of it is never needed: when an item can not start with anything that may follow
// it, giving back can never help; when the branches of an alternation start with different elements, at most one of them
// can match. Analyze finds those places, Apply makes the repetitions there possessive and the alternations orthogonal, and
// the result matches what the original did, with a lot less work.
//
//	result, err := optimize.Optimize(grammar, optimize.Options{})
//	fmt.Print(result) // what was changed, and why the rest was not
//
// The analysis can only reason about patterns it can look into. The declarative leaves (entity.Between, In, Equals,
// vector.Text and the like) say which elements they accept; a leaf made from a function does not, so it counts as
// "could be anything" and keeps the places around it as they are. The report says where that happens.
//
// The element type has to be an integer (runes, bytes, integer tokens).
package optimize

import (
	"fmt"
	"strings"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
)

// Options configure the analysis.
type Options struct {
	// Closed says the pattern is the whole input: nothing can follow it but the end of the stream, so the repetition at
	// the end of the grammar can be made possessive. Without it the pattern may be used inside any other pattern, and
	// the result matches what the original did in every one of them.
	Closed bool
}

// Kind is what a decision is about.
type Kind int

const (
	// Possessive: a repetition that never gives back what it matched.
	Possessive Kind = iota

	// Orthogonal: an alternation that commits to the first branch that matches.
	Orthogonal
)

func (k Kind) String() string {
	if k == Possessive {
		return "possessive"
	}

	return "orthogonal"
}

// Decision is what the analysis found about one repetition or alternation.
type Decision[T, P any] struct {
	Kind Kind

	// Construct is the repetition or alternation as EBNF, and Rule the rule it is part of.
	Construct string
	Rule      string

	// Safe is true if making it possessive or orthogonal does not change what the grammar matches.
	Safe bool

	// Reason says why it is safe, or what is in the way.
	Reason string

	pattern ebnf.Pattern[T, P]
}

// Result is the outcome of the analysis, one decision for each repetition and alternation that is not possessive or
// orthogonal yet.
type Result[T, P any] struct {
	Decisions []Decision[T, P]
}

// Analyze finds the repetitions and alternations of the pattern that can be made possessive or orthogonal without changing
// what it matches, and why the others can not. It does not change anything, see Apply.
func Analyze[T Integer, P any](p ebnf.Pattern[T, P], opts Options) (*Result[T, P], error) {
	g, err := ebnf.Analyze(p)
	if err != nil {
		return nil, err
	}

	a := newAnalysis(g, opts)
	res := &Result[T, P]{}

	var visit func(n *ebnf.Node[T, P], rule string)

	visit = func(n *ebnf.Node[T, P], rule string) {
		switch {
		case n.Kind == ebnf.KindRepeat && !n.Possessive:
			safe, why := a.possessiveSafe(n)
			res.Decisions = append(res.Decisions, Decision[T, P]{Possessive, n.String(), rule, safe, why, n.Pattern})
		case n.Kind == ebnf.KindAlt && !n.Orthogonal:
			safe, why := a.orthogonalSafe(n)
			res.Decisions = append(res.Decisions, Decision[T, P]{Orthogonal, n.String(), rule, safe, why, n.Pattern})
		}

		for _, c := range n.Children {
			visit(c, rule)
		}
	}

	visit(g.Root, "")

	for _, name := range g.Order {
		visit(g.Rules[name].Children[0], name)
	}

	return res, nil
}

// Apply makes every safe repetition possessive and every safe alternation orthogonal, and returns how many patterns it
// changed. It changes the patterns the grammar is made of, so do it once, before the grammar is used. A pattern that is used
// in more than one place is only changed if it is safe in all of them.
func (r *Result[T, P]) Apply() int {
	safe := map[ebnf.Pattern[T, P]]bool{}

	for _, d := range r.Decisions {
		if d.pattern == nil || !canBeKey(d.pattern) {
			continue
		}

		if ok, seen := safe[d.pattern]; !seen {
			safe[d.pattern] = d.Safe
		} else {
			safe[d.pattern] = ok && d.Safe
		}
	}

	changed := 0

	for p, ok := range safe {
		if !ok {
			continue
		}

		switch v := p.(type) {
		case *repetition.Repetition[T, P]:
			v.SetPossessive(true)
			changed++
		case *alternation.Alternation[T, P]:
			v.SetOrthogonal(true)
			changed++
		}
	}

	return changed
}

// canBeKey: a pattern can be a map key unless it is a value that holds a slice or a map
func canBeKey[T, P any](p ebnf.Pattern[T, P]) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	_ = map[ebnf.Pattern[T, P]]struct{}{p: {}}

	return true
}

// Force makes every repetition possessive and every alternation orthogonal, safe or not, and returns how many patterns it
// changed. That changes what the grammar matches wherever Blocked says it is not safe: the repetition keeps what it matched
// and the alternation keeps the first branch that matches, whatever follows. It is how a grammar is made deterministic on
// purpose, by someone who knows that is what it means; the code generator accepts such a grammar.
func (r *Result[T, P]) Force() int {
	forced := map[ebnf.Pattern[T, P]]bool{}

	for _, d := range r.Decisions {
		if d.pattern != nil && canBeKey(d.pattern) {
			forced[d.pattern] = true
		}
	}

	changed := 0

	for p := range forced {
		switch v := p.(type) {
		case *repetition.Repetition[T, P]:
			v.SetPossessive(true)
			changed++
		case *alternation.Alternation[T, P]:
			v.SetOrthogonal(true)
			changed++
		}
	}

	return changed
}

// Optimize is Analyze followed by Apply.
func Optimize[T Integer, P any](p ebnf.Pattern[T, P], opts Options) (*Result[T, P], error) {
	res, err := Analyze(p, opts)
	if err != nil {
		return nil, err
	}

	res.Apply()

	return res, nil
}

// Safe returns the decisions that are safe.
func (r *Result[T, P]) Safe() []Decision[T, P] {
	var out []Decision[T, P]

	for _, d := range r.Decisions {
		if d.Safe {
			out = append(out, d)
		}
	}

	return out
}

// Blocked returns the decisions that are not safe, with what is in the way.
func (r *Result[T, P]) Blocked() []Decision[T, P] {
	var out []Decision[T, P]

	for _, d := range r.Decisions {
		if !d.Safe {
			out = append(out, d)
		}
	}

	return out
}

// String is a report, one line for every decision.
func (r *Result[T, P]) String() string {
	var sb strings.Builder

	safe, blocked := r.Safe(), r.Blocked()

	fmt.Fprintf(&sb, "%d can be made possessive or orthogonal, %d can not\n", len(safe), len(blocked))

	for _, group := range []struct {
		title string
		ds    []Decision[T, P]
	}{{"safe", safe}, {"not safe", blocked}} {
		for _, d := range group.ds {
			where := ""
			if d.Rule != "" {
				where = " in " + d.Rule
			}

			fmt.Fprintf(&sb, "  %s %s%s: %s: %s\n", group.title, d.Kind, where, d.Construct, d.Reason)
		}
	}

	return sb.String()
}
