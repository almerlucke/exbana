package exbana

// Reader interface for a stream that can serve objects to a pattern matcher.
// T is the type of the returned objects in the stream, P is the position type used.
type Reader[T, P any] interface {
	Peek1() (T, error)
	Read1() (T, error)
	Peek(int, []T) (int, error)
	Read(int, []T) (int, error)
	Skip(int) (int, error)
	Finished() bool
	Position() (P, error)
	SetPosition(P) error
	Range(P, P) ([]T, error)
	Length(P, P) int
}
