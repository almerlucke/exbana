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

// Memo remembers which patterns are known to have no match at a position.
// A reader that implements Memo enables failure memoization for composite patterns.
type Memo[T, P any] interface {
	// KnownFail returns true if the pattern is known to have no match at the position.
	KnownFail(Pattern[T, P], P) bool

	// RecordFail records that the pattern has no match at the position.
	RecordFail(Pattern[T, P], P)

	// Index maps a position to a unique integer.
	Index(P) int
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
	if !ok {
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
// skips it when p is known to fail at the current position and records the failure when p offered no candidate at all.
// A failure is only recorded if the continuation was never called, because a rejection by the continuation says
// something about the context and not about the pattern itself.
func MatchMemo[T, P any](p Pattern[T, P], r Reader[T, P], k Continuation[T, P], f func(Reader[T, P], Continuation[T, P]) (bool, error)) (bool, error) {
	memo, ok := r.(Memo[T, P])
	if !ok {
		return f(r, k)
	}

	pos, err := r.Position()
	if IsStreamError(err) {
		return false, err
	}

	if memo.KnownFail(p, pos) {
		return false, nil
	}

	offered := false

	matched, err := f(r, func(m *Match[T, P]) (bool, error) {
		offered = true
		return k(m)
	})
	if err == nil && !matched && !offered {
		memo.RecordFail(p, pos)
	}

	return matched, err
}

type failKey[T, P any] struct {
	pattern Pattern[T, P]
	index   int
}

// MemoReader wraps a reader and adds failure memoization. Use one MemoReader per input, it assumes the
// underlying data does not change while it is in use. Memory use is bounded by patterns x positions.
type MemoReader[T, P any] struct {
	Reader[T, P]
	index func(P) int
	fails map[failKey[T, P]]struct{}
}

// WithMemo wraps a reader to enable failure memoization. The index function maps a position to a unique
// integer (for the runes reader: func(p runes.Pos) int { return p.Index }).
func WithMemo[T, P any](r Reader[T, P], index func(P) int) *MemoReader[T, P] {
	return &MemoReader[T, P]{Reader: r, index: index, fails: make(map[failKey[T, P]]struct{})}
}

func (m *MemoReader[T, P]) Index(pos P) int {
	return m.index(pos)
}

func (m *MemoReader[T, P]) KnownFail(p Pattern[T, P], pos P) bool {
	_, ok := m.fails[failKey[T, P]{p, m.index(pos)}]
	return ok
}

func (m *MemoReader[T, P]) RecordFail(p Pattern[T, P], pos P) {
	m.fails[failKey[T, P]{p, m.index(pos)}] = struct{}{}
}
