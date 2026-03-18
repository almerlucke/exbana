package exbana

// Reader is an interface for a stream that can serve objects to a pattern matcher.
// T is the type of the returned objects in the stream, and P is the position type used.
type Reader[T, P any] interface {
	// Peek1 returns the next object in the stream without advancing the position.
	Peek1() (T, error)

	// Read1 returns the next object in the stream and advances the position.
	Read1() (T, error)

	// Peek returns the next n objects in the stream without advancing the position.
	// It fills the provided slice p and returns the number of objects copied.
	Peek(int, []T) (int, error)

	// Read returns the next n objects in the stream and advances the position.
	// It fills the provided slice p and returns the number of objects copied.
	Read(int, []T) (int, error)

	// Skip advances the position by n objects.
	// It returns the number of objects skipped.
	Skip(int) (int, error)

	// Finished returns true if the end of the stream has been reached.
	Finished() bool

	// Position returns the current position in the stream.
	Position() (P, error)

	// SetPosition sets the current position in the stream.
	SetPosition(P) error

	// Range returns a slice of objects between the specified positions.
	Range(P, P) ([]T, error)

	// Length returns the number of objects between the specified positions.
	Length(P, P) int
}
