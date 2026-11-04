package repetition

import (
	"fmt"
	"io"
	"slices"

	ebnf "github.com/almerlucke/exbana/v3"
)

// Repetition matches a sub-pattern multiple times.
// By default, it's greedy and allows backtracking in concatenation patterns.
// If Possessive is set to true, it will not give back matches during backtracking.
type Repetition[T, P any] struct {
	ebnf.Label
	pattern      ebnf.Pattern[T, P]
	min          int
	max          int
	maxGen       int
	possessive   bool
	countWeights []float64
	countFunc    func(*ebnf.GenContext) int
}

// New creates a new repetition pattern
func New[T, P any](pattern ebnf.Pattern[T, P], min int, max int) *Repetition[T, P] {
	rep := &Repetition[T, P]{
		pattern: pattern,
		min:     min,
		max:     max,
		maxGen:  1,
	}

	return rep
}

// Optional is zero or one repetitions of the pattern, printed as x?.
func Optional[T, P any](pattern ebnf.Pattern[T, P]) *Repetition[T, P] {
	return New[T, P](pattern, 0, 1)
}

// Any is zero or more repetitions of the pattern, printed as x*.
func Any[T, P any](pattern ebnf.Pattern[T, P]) *Repetition[T, P] {
	return New[T, P](pattern, 0, 0)
}

// OneOrMore is one or more repetitions of the pattern, printed as x+.
func OneOrMore[T, P any](pattern ebnf.Pattern[T, P]) *Repetition[T, P] {
	return New[T, P](pattern, 1, 0)
}

// Possessive returns true if the repetition never gives back what it matched, see SetPossessive.
func (rep *Repetition[T, P]) Possessive() bool {
	return rep.possessive
}

// SetPossessive makes the repetition possessive: it matches as many repetitions as it can in a loop and offers only that match, without backtracking into it. That is faster and uses no stack per repetition, so it is the right choice where the repeated pattern can not overlap with what follows.
func (rep *Repetition[T, P]) SetPossessive(possessive bool) *Repetition[T, P] {
	rep.possessive = possessive
	return rep
}

// Min returns the least number of repetitions.
func (rep *Repetition[T, P]) Min() int {
	return rep.min
}

// Max returns the greatest number of repetitions, zero means no limit.
func (rep *Repetition[T, P]) Max() int {
	return rep.max
}

// Match matches the repetition pattern aginst a stream
func (rep *Repetition[T, P]) Match(r ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	return ebnf.FirstMatch(r, rep.MatchCont)
}

// MatchCont offers the matches of the repetition to the continuation, most repetitions first (greedy). If the
// continuation rejects a match the repetition gives back one repetition at a time until min is reached.
// A possessive repetition does not backtrack, it only offers the greedy match and is faster as it does not recurse.
func (rep *Repetition[T, P]) MatchCont(r ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	return ebnf.MatchMemo(ebnf.Pattern[T, P](rep), r, k, rep.matchCont)
}

func (rep *Repetition[T, P]) matchCont(r ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	beginPos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	if rep.possessive {
		return rep.matchPossessive(r, beginPos, k)
	}

	call := &repeatCall[T, P]{rep: rep, r: r, k: k, beginPos: beginPos}

	// a memoizing reader remembers the ways the repeated pattern can match, which lets the search go through them
	// with a stack of its own instead of one nested call per repetition
	if ebnf.InitFailSet(&call.failed, r) {
		return call.iterate()
	}

	return call.next()
}

// repeatCall is the state of one MatchCont call of a non possessive repetition.
type repeatCall[T, P any] struct {
	rep      *Repetition[T, P]
	r        ebnf.Reader[T, P]
	k        ebnf.Continuation[T, P]
	beginPos P

	// matches holds the match of every repetition so far. When the repetition is complete the slice itself is handed
	// to the continuation, which saves a copy on the way that succeeds. It is then shared, and has to be copied before
	// backtracking changes it.
	matches []*ebnf.Match[T, P]
	shared  bool

	failed ebnf.FailSet[P]
}

// add records the match of repetition n, forgetting the ones after it
func (c *repeatCall[T, P]) add(n int, m *ebnf.Match[T, P]) {
	if c.shared {
		c.matches = slices.Clone(c.matches[:n:n])
		c.shared = false
	}

	c.matches = append(c.matches[:n], m)
}

// complete offers the repetitions matched so far to the continuation
func (c *repeatCall[T, P]) complete(n int, end P) (bool, error) {
	c.shared = true

	return c.k(ebnf.NewMatch(c.rep, c.beginPos, end, nil, c.matches[:n:n]))
}

// next is called with the reader positioned after the repetitions matched so far
func (c *repeatCall[T, P]) next() (bool, error) {
	herePos, err := c.r.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	// once min is reached and there is no max, the outcome only depends on the position and not on the
	// number of repetitions, so a dead end at this position stays a dead end
	stateless := c.rep.max == 0 && len(c.matches) >= c.rep.min

	if stateless && c.failed.Has(0, herePos) {
		return false, nil
	}

	matched, err := c.nextFrom(herePos)
	if err == nil && !matched && stateless {
		c.failed.Add(0, herePos)
	}

	return matched, err
}

// iterate does what next does, in the same order and with the same dead end checks, but without a nested call for
// every repetition: a list of candidates per level takes the place of the call stack. That keeps the stack flat for
// long repetitions (a list of a hundred thousand items), and the garbage collector does not have to walk a deep stack.
// It needs the matches of the repeated pattern as a list, which only a memoizing reader has.
func (c *repeatCall[T, P]) iterate() (bool, error) {
	rep, r := c.rep, c.r

	// level n is the position after n repetitions, with the matches of the next repetition still to try
	type level struct {
		pos   P
		cands []*ebnf.Match[T, P]
		next  int
	}

	var levels []level

	push := func(pos P) error {
		var cands []*ebnf.Match[T, P]

		if n := len(levels); rep.max == 0 || n < rep.max {
			if err := r.SetPosition(pos); ebnf.IsStreamError(err) {
				return err
			}

			var err error

			if cands, err = ebnf.DistinctMatches(rep.pattern, r); err != nil {
				return err
			}
		}

		levels = append(levels, level{pos: pos, cands: cands})

		return nil
	}

	if err := push(c.beginPos); err != nil {
		return false, err
	}

	for len(levels) > 0 {
		n := len(levels) - 1
		lv := &levels[n]

		if lv.next < len(lv.cands) {
			m := lv.cands[lv.next]
			lv.next++

			c.add(n, m)

			// a repetition that consumed nothing would repeat forever, so it ends the repetition and counts
			// as all the repetitions that are still needed
			if r.Length(lv.pos, m.End) <= 0 {
				if matched, err := c.completeAt(n+1, m.End); err != nil || matched {
					return matched, err
				}

				continue
			}

			// once min is reached and there is no max, a dead end at this position stays a dead end
			if rep.max == 0 && n+1 >= rep.min && c.failed.Has(0, m.End) {
				continue
			}

			if err := push(m.End); err != nil {
				return false, err
			}

			continue
		}

		// no more repetitions to try from here: give back, offering the repetitions so far
		pos := lv.pos
		levels = levels[:n]
		c.matches = c.matches[:n]

		if n < rep.min {
			ebnf.LogMismatch(r, ebnf.Pattern[T, P](rep), func() *ebnf.Mismatch[T, P] {
				return ebnf.NewMismatch(rep, c.beginPos, pos, nil, slices.Clone(c.matches[:n]))
			})

			continue
		}

		if matched, err := c.completeAt(n, pos); err != nil || matched {
			return matched, err
		}

		if rep.max == 0 {
			c.failed.Add(0, pos)
		}
	}

	return false, nil
}

// completeAt is complete for a search that does not leave the reader where the repetitions end
func (c *repeatCall[T, P]) completeAt(n int, end P) (bool, error) {
	if err := c.r.SetPosition(end); ebnf.IsStreamError(err) {
		return false, err
	}

	return c.complete(n, end)
}

func (c *repeatCall[T, P]) nextFrom(herePos P) (bool, error) {
	rep, r := c.rep, c.r
	n := len(c.matches)

	if rep.max == 0 || n < rep.max {
		matched, err := rep.pattern.MatchCont(r, func(m *ebnf.Match[T, P]) (bool, error) {
			c.add(n, m)

			// a repetition that consumed nothing would repeat forever, so it ends the repetition and counts
			// as all the repetitions that are still needed
			if r.Length(herePos, m.End) <= 0 {
				return c.complete(n+1, m.End)
			}

			return c.next()
		})
		if err != nil || matched {
			return matched, err
		}

		c.matches = c.matches[:n]

		err = r.SetPosition(herePos)
		if ebnf.IsStreamError(err) {
			return false, err
		}
	}

	if n < rep.min {
		ebnf.LogMismatch(r, ebnf.Pattern[T, P](rep), func() *ebnf.Mismatch[T, P] {
			return ebnf.NewMismatch(rep, c.beginPos, herePos, nil, slices.Clone(c.matches[:n]))
		})
		return false, nil
	}

	return c.complete(n, herePos)
}

func (rep *Repetition[T, P]) matchPossessive(r ebnf.Reader[T, P], beginPos P, k ebnf.Continuation[T, P]) (bool, error) {
	var matches []*ebnf.Match[T, P]

	for rep.max == 0 || len(matches) < rep.max {
		resetPos, err := r.Position()
		if ebnf.IsStreamError(err) {
			return false, err
		}

		matched, result, err := ebnf.Parse(rep.pattern, r)
		if err != nil {
			return false, err
		}

		if !matched {
			err = r.SetPosition(resetPos)
			if ebnf.IsStreamError(err) {
				return false, err
			}

			break
		}

		matches = append(matches, result)

		// a match that consumed nothing would match forever, stop repeating
		if r.Length(resetPos, result.End) <= 0 {
			break
		}
	}

	endPos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	if len(matches) < rep.min {
		ebnf.LogMismatch(r, ebnf.Pattern[T, P](rep), func() *ebnf.Mismatch[T, P] { return ebnf.NewMismatch(rep, beginPos, endPos, nil, matches) })
		return false, nil
	}

	return k(ebnf.NewMatch(rep, beginPos, endPos, nil, matches))
}

// SetMaxGen sets the maximum generated entities on top of min
func (rep *Repetition[T, P]) SetMaxGen(maxGen int) *Repetition[T, P] {
	rep.maxGen = maxGen
	return rep
}

// SetCountWeights sets the relative odds of the number of repetitions when generating: weights[i] is the weight of
// min+i repetitions. With a max, min+len(weights)-1 must not be more than max. Weights do not influence matching.
func (rep *Repetition[T, P]) SetCountWeights(weights ...float64) *Repetition[T, P] {
	rep.countWeights = weights
	return rep
}

// SetCountFunc sets a function that decides the number of repetitions each time the repetition generates, for counts
// that depend on the generation state or depth. The count must be between min and max (if there is a max).
// It takes precedence over SetCountWeights and SetMaxGen.
func (rep *Repetition[T, P]) SetCountFunc(f func(*ebnf.GenContext) int) *Repetition[T, P] {
	rep.countFunc = f
	return rep
}

// Generate writes pattern to a writer a random number of times
func (rep *Repetition[T, P]) Generate(ctx *ebnf.GenContext, w ebnf.Writer[T]) error {
	if err := ctx.Enter(); err != nil {
		return err
	}
	defer ctx.Leave()

	n, err := rep.generateCount(ctx)
	if err != nil {
		return err
	}

	for i := 0; i < n; i++ {
		err := rep.pattern.Generate(ctx, w)
		if err != nil {
			return err
		}
	}

	return nil
}

func (rep *Repetition[T, P]) generateCount(ctx *ebnf.GenContext) (int, error) {
	var n int

	switch {
	case rep.countFunc != nil:
		n = rep.countFunc(ctx)
	case rep.countWeights != nil:
		i, err := ctx.Choose(rep.countWeights)
		if err != nil {
			return 0, err
		}

		n = rep.min + i
	default:
		repMax := rep.max
		if rep.max == 0 {
			repMax = rep.min + rep.maxGen
		}

		n = ctx.Intn(repMax+1-rep.min) + rep.min
	}

	if n < rep.min || (rep.max != 0 && n > rep.max) {
		return 0, fmt.Errorf("exbana: repetition count %d is outside min %d and max %d", n, rep.min, rep.max)
	}

	return n, nil
}

// printAny prints EBNF zero or more
func (rep *Repetition[T, P]) printAny(w io.Writer) error {
	err := ebnf.PrintChild(w, rep.pattern)
	if err != nil {
		return err
	}

	_, err = w.Write([]byte("*"))

	return err
}

// printAny prints EBNF optional
func (rep *Repetition[T, P]) printOptional(w io.Writer) error {
	err := ebnf.PrintChild(w, rep.pattern)
	if err != nil {
		return err
	}

	_, err = w.Write([]byte("?"))

	return err
}

// printAny prints EBNF at least one
func (rep *Repetition[T, P]) printAtLeastOne(w io.Writer) error {
	err := ebnf.PrintChild(w, rep.pattern)
	if err != nil {
		return err
	}

	_, err = w.Write([]byte("+"))

	return err
}

// Print EBNF repetition pattern
func (rep *Repetition[T, P]) Print(w io.Writer) error {
	if rep.min == 0 && rep.max == 0 {
		return rep.printAny(w)
	} else if rep.min == 0 && rep.max == 1 {
		return rep.printOptional(w)
	} else if rep.min == 1 && rep.max == 0 {
		return rep.printAtLeastOne(w)
	}

	var err error

	// n or more: "n * x, x*"
	if rep.max == 0 {
		_, err = w.Write([]byte(fmt.Sprintf("(%v * ", rep.min)))
		if err != nil {
			return err
		}

		err = ebnf.PrintChild(w, rep.pattern)
		if err != nil {
			return err
		}

		_, err = w.Write([]byte(", "))
		if err != nil {
			return err
		}

		err = rep.printAny(w)
		if err != nil {
			return err
		}

		_, err = w.Write([]byte(")"))

		return err
	}

	oneValue := rep.min == rep.max

	if !oneValue {
		_, err = w.Write([]byte("("))
		if err != nil {
			return err
		}
	}

	_, err = w.Write([]byte(fmt.Sprintf("%v * ", rep.min)))
	if err != nil {
		return err
	}

	err = ebnf.PrintChild(w, rep.pattern)
	if err != nil {
		return err
	}

	if !oneValue {
		_, err = w.Write([]byte(fmt.Sprintf(", %v * ", rep.max-rep.min)))
		if err != nil {
			return err
		}

		err = ebnf.PrintChild(w, rep.pattern)
		if err != nil {
			return err
		}

		_, err = w.Write([]byte("?)"))
		if err != nil {
			return err
		}
	}

	return nil
}

// Children returns the repeated pattern
func (rep *Repetition[T, P]) Children() ebnf.Patterns[T, P] {
	return ebnf.Patterns[T, P]{rep.pattern}
}

// Named sets the identifier of the pattern and returns the pattern, so it can be chained: names are what syntax
// errors and printed grammars call the pattern.
func (rep *Repetition[T, P]) Named(id string) *Repetition[T, P] {
	rep.SetID(id)
	return rep
}

// Printed sets the text the pattern prints as and returns the pattern.
func (rep *Repetition[T, P]) Printed(text string) *Repetition[T, P] {
	rep.SetText(text)
	return rep
}
