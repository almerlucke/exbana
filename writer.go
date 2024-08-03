package exbana

// Writer interface to write generated objects
type Writer[T any] interface {
	Write(...T) error
	Finish() error
}
