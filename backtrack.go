package exbana

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

// Memo remembers, for a pattern at a position, the distinct ways it can end. A reader that implements Memo (a
// ParseContext with WithMemo) enables memoization for composite patterns.
type Memo[T, P any] interface {
	// Solutions returns the matches remembered for the pattern at the position, one per distinct end position in
	// order of preference. The second result is false if nothing is remembered yet. An empty list means the pattern
	// is known to have no match there.
	Solutions(Pattern[T, P], P) ([]*Match[T, P], bool)

	// SetSolutions remembers the matches of the pattern at the position.
	SetSolutions(Pattern[T, P], P, []*Match[T, P])

	// Index maps a position to a unique integer.
	Index(P) int

	// Enabled returns true if the reader actually memoizes.
	Enabled() bool
}

// FailSet remembers the positions at which a search with one fixed continuation already failed, so the same dead
// end is not explored again. It is only valid during a single MatchCont call, because the continuation is fixed
// there. A nil FailSet (no memoization) is valid and remembers nothing.
type FailSet[P any] struct {
	index func(P) int
	set   map[[2]int]struct{}
}

// NewFailSet creates a FailSet if the reader supports memoization, otherwise it returns nil.
func NewFailSet[T, P any](r Reader[T, P]) *FailSet[P] {
	memo, ok := r.(Memo[T, P])
	if !ok || !memo.Enabled() {
		return nil
	}

	return &FailSet[P]{index: memo.Index, set: make(map[[2]int]struct{})}
}

// Has returns true if the search was already found to fail for this state (a caller defined number) at the position.
func (f *FailSet[P]) Has(state int, pos P) bool {
	if f == nil {
		return false
	}

	_, ok := f.set[[2]int{state, f.index(pos)}]

	return ok
}

// Add records that the search fails for this state at the position.
func (f *FailSet[P]) Add(state int, pos P) {
	if f != nil {
		f.set[[2]int{state, f.index(pos)}] = struct{}{}
	}
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
	memo, ok := r.(Memo[T, P])
	if !ok || !memo.Enabled() {
		return f(r, k)
	}

	pos, err := r.Position()
	if IsStreamError(err) {
		return false, err
	}

	// while a left recursive rule grows its seed the matches of patterns at its position change with every round
	if g, ok := r.(interface{ GrowingAt(P) bool }); ok && g.GrowingAt(pos) {
		return f(r, k)
	}

	solutions, known := memo.Solutions(p, pos)
	if !known {
		seen := make(map[int]struct{})

		_, err = f(r, func(m *Match[T, P]) (bool, error) {
			end := memo.Index(m.End)
			if _, dup := seen[end]; !dup {
				seen[end] = struct{}{}
				solutions = append(solutions, m)
			}

			return false, nil
		})
		if err != nil {
			return false, err
		}

		memo.SetSolutions(p, pos, solutions)
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
