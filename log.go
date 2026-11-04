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

// MismatchFilter is an optional extension of Logger. A logger that implements it is asked whether it wants the
// mismatches of a pattern before they are built, and building a mismatch costs allocations on every failed
// alternative of a parse. FarthestLog uses it to skip the patterns it would ignore anyway.
type MismatchFilter[T, P any] interface {
	WantsMismatch(Pattern[T, P]) bool
}

// FailureLogger is an optional extension of Logger for loggers that only need to know where a pattern failed and
// not what it had matched before, which is all a leaf pattern has to tell. It is told through LogFailure, so no
// Mismatch is built.
type FailureLogger[T, P any] interface {
	LogFailure(p Pattern[T, P], begin P)
}
