package exbana

// Logger provides an interface for logging pattern match results and mismatches.
// It is useful for debugging and tracing the parsing process.
type Logger[T, P any] interface {
	// LogMismatch logs a pattern mismatch event.
	LogMismatch(*Mismatch[T, P])
}

// VoidLog is a no-op logger that discards all log events.
type VoidLog[T, P any] struct{}

// NewVoidLog creates a new VoidLog instance.
func NewVoidLog[T, P any]() *VoidLog[T, P] {
	return &VoidLog[T, P]{}
}

// LogMismatch implements the Logger interface for VoidLog (it does nothing).
func (l *VoidLog[T, P]) LogMismatch(_ *Mismatch[T, P]) {
	/* void */
}

// StackLog is a logger that stores mismatch events in a slice.
type StackLog[T, P any] struct {
	Stack []*Mismatch[T, P]
}

// NewStackLog creates a new StackLog instance.
func NewStackLog[T, P any]() *StackLog[T, P] {
	return &StackLog[T, P]{}
}

// LogMismatch implements the Logger interface for StackLog.
func (s *StackLog[T, P]) LogMismatch(m *Mismatch[T, P]) {
	s.Stack = append(s.Stack, m)
}

// LongestMatch returns the mismatch that resulted in the longest sequence of matched objects.
// This is often where the most significant error occurred in a failed parse.
func (s *StackLog[T, P]) LongestMatch() *Mismatch[T, P] {
	var lm *Mismatch[T, P]
	var l int
	for _, m := range s.Stack {
		if len(m.Matched) > l {
			lm = m
			l = len(m.Matched)
		}
	}
	return lm
}
