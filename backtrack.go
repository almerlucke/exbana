package exbana

import "errors"

// Continuation receives a candidate match of a pattern, with the reader positioned at the end of that match.
// It returns true to accept the candidate (matching stops, the reader stays where it is), or false to reject it,
// in which case the pattern backtracks and offers its next candidate (if any).
type Continuation[T, P any] func(*Match[T, P]) (bool, error)

// FirstMatch runs a continuation based matcher and accepts the first candidate it offers. It is the bridge between
// MatchCont and Match: on success the reader is positioned at the end of the returned match.
func FirstMatch[T, P any](r Reader[T, P], f func(Reader[T, P], Continuation[T, P]) (bool, error)) (bool, *Match[T, P], error) {
	var result *Match[T, P]

	matched, err := f(r, func(m *Match[T, P]) (bool, error) {
		result = m
		return true, nil
	})
	if err != nil || !matched {
		return false, nil, err
	}

	return true, result, nil
}

// memoizer is implemented by a ParseContext, and is enabled by WithMemo. It remembers, for a pattern at a position, the
// distinct ways the pattern can end.
type memoizer[T, P any] interface {
	// solutions returns the matches remembered for the pattern at the position, one per distinct end position in
	// order of preference. The second result is false if nothing is remembered yet. An empty list means the pattern
	// is known to have no match there.
	solutions(Pattern[T, P], P) ([]*Match[T, P], bool)

	// setSolutions remembers the matches of the pattern at the position.
	setSolutions(Pattern[T, P], P, []*Match[T, P])

	// indexOf maps a position to a unique integer.
	indexOf(P) int

	// memoEnabled returns true if the reader actually memoizes.
	memoEnabled() bool
}

// FailSet remembers the positions at which a search with one fixed continuation already failed, so the same dead
// end is not explored again. It is only valid during a single MatchCont call, because the continuation is fixed
// there. A nil FailSet (no memoization) is valid and remembers nothing.
type FailSet[P any] struct {
	index func(P) int
	set   map[[2]int]struct{}
}

// InitFailSet prepares a FailSet that is a field of a larger struct, which saves allocating it separately, and
// returns false if the reader does not memoize. A FailSet that was not initialized remembers nothing, like a nil one.
func InitFailSet[T, P any](f *FailSet[P], r Reader[T, P]) bool {
	memo, ok := r.(memoizer[T, P])
	if !ok || !memo.memoEnabled() {
		return false
	}

	f.index = memo.indexOf

	return true
}

func (f *FailSet[P]) enabled() bool {
	return f != nil && f.index != nil
}

// Has returns true if the search was already found to fail for this state (a caller defined number) at the position.
func (f *FailSet[P]) Has(state int, pos P) bool {
	if !f.enabled() {
		return false
	}

	_, ok := f.set[[2]int{state, f.index(pos)}]

	return ok
}

// Add records that the search fails for this state at the position.
func (f *FailSet[P]) Add(state int, pos P) {
	if !f.enabled() {
		return
	}

	// most searches never fail, so the map is made when the first dead end is found
	if f.set == nil {
		f.set = make(map[[2]int]struct{})
	}

	f.set[[2]int{state, f.index(pos)}] = struct{}{}
}

// MatchMemo runs f (the continuation based matcher of pattern p) and, if the reader supports memoization,
// remembers the ways p can match at the current position, so that the next time p is tried there the work is not
// done again.
//
// What is remembered is one match per distinct end position, in the order f offered them. That is enough because
// whether the rest of a parse succeeds depends on where p ended and not on how it matched, so a second match with the
// same end could only fail the same way. The first one is the preferred one, so the result of a parse does not change.
// Without this, a pattern with many derivations that end at the same position (nested alternations that can match
// the same text in different ways) takes exponential time when what follows fails.
//
// Remembering all ways means f runs to completion the first time instead of stopping at the first match that is
// accepted; the cost is bounded by the number of positions, not by the number of derivations.
func MatchMemo[T, P any](p Pattern[T, P], r Reader[T, P], k Continuation[T, P], f func(Reader[T, P], Continuation[T, P]) (bool, error)) (bool, error) {
	memo, ok := r.(memoizer[T, P])
	if !ok || !memo.memoEnabled() {
		return f(r, k)
	}

	pos, err := r.Position()
	if IsStreamError(err) {
		return false, err
	}

	// while a left recursive rule grows its seed the matches of patterns at its position change with every round
	if g, ok := r.(interface{ growingAt(P) bool }); ok && g.growingAt(pos) {
		return f(r, k)
	}

	solutions, known := memo.solutions(p, pos)
	if !known {
		solutions, err = collectDistinct(r, memo, f)
		if err != nil {
			return false, err
		}

		memo.setSolutions(p, pos, solutions)
	}

	for _, m := range solutions {
		if err := r.SetPosition(m.End); IsStreamError(err) {
			return false, err
		}

		matched, err := k(m)
		if err != nil || matched {
			return matched, err
		}
	}

	return false, nil
}

// distinctCollector collects the matches offered to it, one for each end position.
type distinctCollector[T, P any] struct {
	memo      memoizer[T, P]
	solutions []*Match[T, P]

	// most patterns end in one or two ways, so duplicates are looked for by scanning, and a set is only made when there
	// are many
	seen map[int]struct{}
}

func (d *distinctCollector[T, P]) offer(m *Match[T, P]) (bool, error) {
	end := d.memo.indexOf(m.End)

	if d.seen == nil {
		for _, s := range d.solutions {
			if d.memo.indexOf(s.End) == end {
				return false, nil
			}
		}

		d.solutions = append(d.solutions, m)

		if len(d.solutions) > 8 {
			d.seen = make(map[int]struct{}, 2*len(d.solutions))
			for _, s := range d.solutions {
				d.seen[d.memo.indexOf(s.End)] = struct{}{}
			}
		}

		return false, nil
	}

	if _, dup := d.seen[end]; !dup {
		d.seen[end] = struct{}{}
		d.solutions = append(d.solutions, m)
	}

	return false, nil
}

// collectDistinct runs f and returns the matches it offers, one for each end position, in the order they were offered.
func collectDistinct[T, P any](r Reader[T, P], memo memoizer[T, P], f func(Reader[T, P], Continuation[T, P]) (bool, error)) ([]*Match[T, P], error) {
	d := &distinctCollector[T, P]{memo: memo}

	_, err := f(r, d.offer)

	return d.solutions, err
}

// DistinctMatches returns the ways the pattern matches at the current position of the reader, one for each end
// position, in order of preference. A memoizing reader remembers the answer for composite patterns, so asking again is
// cheap, and it is how a pattern that repeats another one can walk through the matches without recursing.
// The position of the reader is undefined afterwards.
func DistinctMatches[T, P any](p Pattern[T, P], r Reader[T, P]) ([]*Match[T, P], error) {
	memo, ok := r.(memoizer[T, P])
	if !ok || !memo.memoEnabled() {
		return nil, errNoMemo
	}

	pos, err := r.Position()
	if IsStreamError(err) {
		return nil, err
	}

	growing := false
	if g, ok := r.(interface{ growingAt(P) bool }); ok {
		growing = g.growingAt(pos)
	}

	if !growing {
		if solutions, known := memo.solutions(p, pos); known {
			return solutions, nil
		}
	}

	return collectDistinct(r, memo, p.MatchCont)
}

// errNoMemo is returned by DistinctMatches for a reader that does not memoize.
var errNoMemo = errors.New("exbana: the reader does not memoize")
