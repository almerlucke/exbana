package exbana

import (
	"iter"
	"slices"
)

// Matches returns an iterator over every way the pattern matches at the current position of the reader, in order of
// preference: the first match is the one Parse returns. Each iteration yields a match and a nil error, or, if the
// matcher fails, a nil match and the error, which ends the iteration. Stopping early with break or return stops the
// matcher, there is no work done for the matches you do not ask for.
//
//	for m, err := range ebnf.Matches(pattern, rd) {
//		if err != nil { ... }
//		fmt.Println(m.End)
//	}
//
// While the loop body runs the reader is positioned at the end of the match, so it can be used to look at the match;
// it must not be repositioned. Reading ahead and restoring the position is fine. With a memoizing ParseContext every
// match has a different end position, matches that only differ in how they got to the same end are left out.
//
// Matches is how to find out whether a grammar is ambiguous for some input: ask for the matches of the grammar followed
// by an end pattern, more than one means more than one parse.
func Matches[T, P any](p Pattern[T, P], r Reader[T, P]) iter.Seq2[*Match[T, P], error] {
	return func(yield func(*Match[T, P], error) bool) {
		stopped := false

		_, err := p.MatchCont(r, func(m *Match[T, P]) (bool, error) {
			if !yield(m, nil) {
				stopped = true

				// accepting the candidate is how the matcher is told to stop
				return true, nil
			}

			return false, nil
		})
		if err != nil && !stopped {
			yield(nil, err)
		}
	}
}

// EndSet is a set of positions of one stream. It only needs the ordering the reader gives (Length), so positions do
// not have to be comparable or hashable.
type EndSet[T, P any] struct {
	r   Reader[T, P]
	set []P // in stream order
}

// NewEndSet creates an empty set of positions of the stream r reads.
func NewEndSet[T, P any](r Reader[T, P]) *EndSet[T, P] {
	return &EndSet[T, P]{r: r}
}

func (s *EndSet[T, P]) find(pos P) (int, bool) {
	return slices.BinarySearchFunc(s.set, pos, func(e, target P) int {
		// negative when e comes before target
		return -s.r.Length(e, target)
	})
}

// Has returns true if the position is in the set.
func (s *EndSet[T, P]) Has(pos P) bool {
	_, ok := s.find(pos)
	return ok
}

// Add adds a position and returns true if it was not in the set yet.
func (s *EndSet[T, P]) Add(pos P) bool {
	i, ok := s.find(pos)
	if ok {
		return false
	}

	s.set = slices.Insert(s.set, i, pos)

	return true
}
