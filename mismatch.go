package exbana

// Mismatch can hold information about a pattern mismatch and possibly the sub pattern that caused the mismatch
// and the sub patterns that matched so far
type Mismatch[T, P any] struct {
	Pattern   Pattern[T, P]
	Begin     P
	End       P
	Unmatched *Match[T, P]
	Matched   []*Match[T, P]
}

// NewMismatch creates a new pattern mismatch
func NewMismatch[T, P any](pattern Pattern[T, P], begin P, end P, unmatched *Match[T, P], matched []*Match[T, P]) *Mismatch[T, P] {
	return &Mismatch[T, P]{
		Pattern:   pattern,
		Begin:     begin,
		End:       end,
		Unmatched: unmatched,
		Matched:   matched,
	}
}
