package exbana

// ParseContext is a reader with everything a parse needs besides the grammar: where mismatches are logged and the
// failure memo. Patterns find it through the reader they are given, so the grammar itself is never changed by a
// parse. That makes one grammar usable by many parses at the same time, each with its own context.
//
//	ctx := ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log).WithMemo(indexFn)
//	matched, match, err := grammar.Match(ctx)
//
// A ParseContext is for one parse of one input and is not safe for concurrent use. It assumes the underlying data
// does not change while it is in use.
type ParseContext[T, P any] struct {
	Reader[T, P]
	logger Logger[T, P]
	index  func(P) int
	fails  map[failKey[T, P]]struct{}
}

type failKey[T, P any] struct {
	pattern Pattern[T, P]
	index   int
}

// NewParseContext wraps a reader in a parse context. Without further setup it does nothing but forward to the reader.
func NewParseContext[T, P any](r Reader[T, P]) *ParseContext[T, P] {
	return &ParseContext[T, P]{Reader: r}
}

// WithLogger sets the logger that receives the mismatches of this parse. It takes precedence over the loggers of
// the patterns (see Pattern.SetLogger), which are only used when the context has no logger.
func (c *ParseContext[T, P]) WithLogger(logger Logger[T, P]) *ParseContext[T, P] {
	c.logger = logger
	return c
}

// WithMemo enables failure memoization. The index function maps a position to a unique integer
// (for the runes reader: func(p runes.Pos) int { return p.Index }). Memory use is bounded by patterns x positions.
func (c *ParseContext[T, P]) WithMemo(index func(P) int) *ParseContext[T, P] {
	c.index = index
	c.fails = make(map[failKey[T, P]]struct{})

	return c
}

// WithMemo wraps a reader in a parse context with failure memoization, shorthand for NewParseContext(r).WithMemo(index).
func WithMemo[T, P any](r Reader[T, P], index func(P) int) *ParseContext[T, P] {
	return NewParseContext(r).WithMemo(index)
}

// Logger returns the logger of the context, or nil if it has none.
func (c *ParseContext[T, P]) Logger() Logger[T, P] {
	return c.logger
}

// Enabled implements Memo
func (c *ParseContext[T, P]) Enabled() bool {
	return c.fails != nil
}

// Index implements Memo
func (c *ParseContext[T, P]) Index(pos P) int {
	return c.index(pos)
}

// KnownFail implements Memo
func (c *ParseContext[T, P]) KnownFail(p Pattern[T, P], pos P) bool {
	_, ok := c.fails[failKey[T, P]{p, c.index(pos)}]
	return ok
}

// RecordFail implements Memo
func (c *ParseContext[T, P]) RecordFail(p Pattern[T, P], pos P) {
	c.fails[failKey[T, P]{p, c.index(pos)}] = struct{}{}
}

// LogMismatch reports a mismatch of pattern p to the logger of the reader if it is a ParseContext with a logger, and
// otherwise to the logger of the pattern. The mismatch is only built if somebody is listening, which saves an
// allocation for every failed alternative when nothing is logged.
func LogMismatch[T, P any](r Reader[T, P], p Pattern[T, P], build func() *Mismatch[T, P]) {
	var logger Logger[T, P]

	if c, ok := r.(interface{ Logger() Logger[T, P] }); ok {
		logger = c.Logger()
	}

	if logger == nil {
		logger = p.Logger()
	}

	if logger == nil {
		return
	}

	if _, void := logger.(*VoidLog[T, P]); void {
		return
	}

	logger.LogMismatch(build())
}

// Silent returns a reader to use for lookahead, such as the exception of an Except: it reads from the same position as
// r, but a parse context logs nothing through it and does not memoize. Failing is the normal outcome of lookahead, so
// it would only add noise to the log, and a failure recorded here must not hide a mismatch that should be logged
// elsewhere. A reader that is not a ParseContext is returned as is.
func Silent[T, P any](r Reader[T, P]) Reader[T, P] {
	c, ok := r.(*ParseContext[T, P])
	if !ok {
		return r
	}

	quiet := *c
	quiet.logger = NewVoidLog[T, P]()
	quiet.index = nil
	quiet.fails = nil

	return &quiet
}
