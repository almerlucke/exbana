package exbana

// Mismatch stores information about a failed pattern match.
// It can include the pattern that failed, the position, and sub-patterns that either matched or failed.
type Mismatch[T, P any] struct {
	Pattern   Pattern[T, P]  // The pattern that failed to match.
	Begin     P              // The position where the match attempt began.
	End       P              // The position where the match attempt failed.
	Unmatched *Match[T, P]   // The sub-match that caused the overall failure.
	Matched   []*Match[T, P] // Sub-matches that were successful before the failure.
}

// NewMismatch creates a new Mismatch instance.
func NewMismatch[T, P any](pattern Pattern[T, P], begin P, end P, unmatched *Match[T, P], matched []*Match[T, P]) *Mismatch[T, P] {
	return &Mismatch[T, P]{
		Pattern:   pattern,
		Begin:     begin,
		End:       end,
		Unmatched: unmatched,
		Matched:   matched,
	}
}
