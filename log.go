package exbana

type Logger[T, P any] interface {
	LogMismatch(*Mismatch[T, P])
}

type VoidLog[T, P any] struct{}

func NewVoidLog[T, P any]() *VoidLog[T, P] {
	return &VoidLog[T, P]{}
}

func (l *VoidLog[T, P]) LogMismatch(_ *Mismatch[T, P]) {
	/* void */
}

type StackLog[T, P any] struct {
	Stack []*Mismatch[T, P]
}

func NewStackLog[T, P any]() *StackLog[T, P] {
	return &StackLog[T, P]{}
}

func (s *StackLog[T, P]) LogMismatch(m *Mismatch[T, P]) {
	s.Stack = append(s.Stack, m)
}
