package exbana

import "io"

const (
	NoID = ""
)

// Pattern can match objects from a stream, generate objects to write to a stream, print and has an identifier
type Pattern[T, P any] interface {
	// Match matches a sub-pattern against the reader.
	// It returns true if matched, along with the match result.
	Match(Reader[T, P]) (bool, *Match[T, P], error)

	// CanUnpack returns true if the match result for this pattern can be unpacked.
	CanUnpack() bool

	// ID returns the identifier of the pattern.
	ID() string

	// SetID sets the identifier for the pattern.
	SetID(string) Pattern[T, P]

	// Logger returns the logger associated with the pattern.
	Logger() Logger[T, P]

	// SetLogger sets the logger for the pattern.
	SetLogger(Logger[T, P]) Pattern[T, P]

	// Self returns the actual pattern implementation.
	Self() Pattern[T, P]

	// SetSelf sets the actual pattern implementation.
	SetSelf(Pattern[T, P]) Pattern[T, P]

	// SetEvalFunc sets the evaluation function for the pattern.
	SetEvalFunc(func(*Match[T, P], Reader[T, P]) (any, error)) Pattern[T, P]

	// Eval executes the evaluation function for the pattern's match result.
	Eval(*Match[T, P], Reader[T, P]) (any, error)

	// Generate generates output to a writer based on the pattern.
	Generate(Writer[T]) error

	// Print prints the pattern to a writer in EBNF format.
	Print(io.Writer) error

	// PrintAsChild prints the pattern as a sub-pattern of another pattern.
	PrintAsChild(io.Writer) error

	// PrintOutput returns the string to use for EBNF printing.
	PrintOutput() string

	// SetPrintOutput sets the string to use for EBNF printing.
	SetPrintOutput(string) Pattern[T, P]
}

// Patterns is a convenience type for a slice of pattern interfaces
type Patterns[T, P any] []Pattern[T, P]

// BasePattern implements the Pattern interface and can be used to provide all basic functionality
// (i.e. self, id) for other pattern implementers
type BasePattern[T, P any] struct {
	id          string
	self        Pattern[T, P]
	logger      Logger[T, P]
	printOutput string
	evalFunc    func(*Match[T, P], Reader[T, P]) (any, error)
}

func NewBasePattern[T, P any]() *BasePattern[T, P] {
	return &BasePattern[T, P]{
		logger: NewVoidLog[T, P](),
	}
}

func (p *BasePattern[T, P]) Self() Pattern[T, P] {
	return p.self
}

func (p *BasePattern[T, P]) SetSelf(self Pattern[T, P]) Pattern[T, P] {
	p.self = self
	return self
}

func (p *BasePattern[T, P]) ID() string {
	return p.id
}

func (p *BasePattern[T, P]) SetID(id string) Pattern[T, P] {
	p.id = id
	return p.self
}

func (p *BasePattern[T, P]) Logger() Logger[T, P] {
	return p.logger
}

func (p *BasePattern[T, P]) SetLogger(logger Logger[T, P]) Pattern[T, P] {
	p.logger = logger
	return p.self
}

func (p *BasePattern[T, P]) Print(w io.Writer) error {
	_, err := w.Write([]byte(p.self.PrintOutput()))
	return err
}

func (p *BasePattern[T, P]) PrintAsChild(w io.Writer) error {
	if id := p.self.ID(); id != "" {
		_, err := w.Write([]byte(id))
		if err != nil {
			return err
		}
	} else {
		err := p.self.Print(w)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *BasePattern[T, P]) Generate(_ Writer[T]) error {
	return nil
}

func (p *BasePattern[T, P]) CanUnpack() bool {
	return false
}

func (p *BasePattern[T, P]) Match(_ Reader[T, P]) (bool, *Match[T, P], error) {
	return false, nil, nil
}

func (p *BasePattern[T, P]) SetEvalFunc(f func(*Match[T, P], Reader[T, P]) (any, error)) Pattern[T, P] {
	p.evalFunc = f
	return p.self
}

func (p *BasePattern[T, P]) Eval(m *Match[T, P], r Reader[T, P]) (any, error) {
	if p.evalFunc != nil {
		return p.evalFunc(m, r)
	}

	return m.Value, nil
}

func (p *BasePattern[T, P]) PrintOutput() string {
	return p.printOutput
}

func (p *BasePattern[T, P]) SetPrintOutput(output string) Pattern[T, P] {
	p.printOutput = output
	return p.self
}
