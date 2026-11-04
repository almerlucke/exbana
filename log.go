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
