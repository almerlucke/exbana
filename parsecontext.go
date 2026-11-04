package exbana

// ParseContext is a reader with everything a parse needs besides the grammar: where mismatches are logged and the
// failure memo. Patterns find it through the reader they are given, so the grammar itself is never changed by a
// parse. That makes one grammar usable by many parses at the same time, each with its own context.
//
//	ctx := ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log).WithMemo(indexFn)
//	matched, match, err := ebnf.Parse(grammar, ctx)
//
// A ParseContext is for one parse of one input and is not safe for concurrent use. It assumes the underlying data
// does not change while it is in use.
type ParseContext[T, P any] struct {
	Reader[T, P]
	logger Logger[T, P]
	index  func(P) int
	solved map[solutionKey[T, P]][]*Match[T, P]
	frames *frameStack[T, P]
}

// frameStack holds the rules that are being matched, it is shared by a context and its Silent copies.
type frameStack[T, P any] struct {
	frames []*RuleFrame[T, P]
}

// RuleFrame is a rule that is being matched at a position. A rule uses it to notice that it is being entered again
// at the same position without having consumed anything, which is left recursion. Only rules need to know about
// this, see rule.Rule.
type RuleFrame[T, P any] struct {
	pattern Pattern[T, P]
	pos     P

	// Open is true while the rule is working out its matches. It is false while a continuation runs that was handed a
	// match of the rule: the rule is then matched, and may legitimately be entered again at the same position (when
	// it matched the empty input).
	Open bool

	// Grows is true for a rule that handles left recursion by growing a seed.
	Grows bool

	// Seed is what a growing rule hands out to its own left recursive calls.
	Seed []*Match[T, P]

	// Used is set when a left recursive call read the seed.
	Used bool
}

type solutionKey[T, P any] struct {
	pattern Pattern[T, P]
	index   int
}

// NewParseContext wraps a reader in a parse context. Without further setup it does nothing but forward to the reader.
func NewParseContext[T, P any](r Reader[T, P]) *ParseContext[T, P] {
	return &ParseContext[T, P]{Reader: r, frames: &frameStack[T, P]{}}
}

// EnsureContext returns the reader as a ParseContext, wrapping it in a new one if it is a plain reader.
func EnsureContext[T, P any](r Reader[T, P]) *ParseContext[T, P] {
	if c, ok := r.(*ParseContext[T, P]); ok {
		return c
	}

	return NewParseContext(r)
}

// PushFrame records that a rule starts matching at a position. The frame must be popped when the rule is done.
func (c *ParseContext[T, P]) PushFrame(p Pattern[T, P], pos P, grows bool) *RuleFrame[T, P] {
	f := &RuleFrame[T, P]{pattern: p, pos: pos, Open: true, Grows: grows}
	c.frames.frames = append(c.frames.frames, f)

	return f
}

// PopFrame removes the frame, and any frames pushed after it.
func (c *ParseContext[T, P]) PopFrame(f *RuleFrame[T, P]) {
	s := c.frames.frames

	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == f {
			clear(s[i:])
			c.frames.frames = s[:i]

			return
		}
	}
}

// FindOpenFrame returns the open frame of the pattern at the position, or nil. Frames are in stream order from the
// bottom of the stack to the top, so the search stops at the first frame that is before the position.
func (c *ParseContext[T, P]) FindOpenFrame(p Pattern[T, P], pos P) *RuleFrame[T, P] {
	s := c.frames.frames

	for i := len(s) - 1; i >= 0; i-- {
		if c.Length(s[i].pos, pos) > 0 {
			return nil
		}

		if s[i].Open && s[i].pattern == p {
			return s[i]
		}
	}

	return nil
}

// GrowingAt returns true if a rule is growing a seed at the position. Results of patterns at that position depend on
// the seed, which changes while it grows, so they must not be memoized.
func (c *ParseContext[T, P]) GrowingAt(pos P) bool {
	s := c.frames.frames

	for i := len(s) - 1; i >= 0; i-- {
		if c.Length(s[i].pos, pos) > 0 {
			return false
		}

		if s[i].Grows {
			return true
		}
	}

	return false
}

// WithLogger sets the logger that receives the mismatches of this parse.
func (c *ParseContext[T, P]) WithLogger(logger Logger[T, P]) *ParseContext[T, P] {
	c.logger = logger
	return c
}

// WithMemo enables failure memoization. The index function maps a position to a unique integer
// (for the runes reader: func(p runes.Pos) int { return p.Index }). Memory use is bounded by patterns x positions.
func (c *ParseContext[T, P]) WithMemo(index func(P) int) *ParseContext[T, P] {
	c.index = index
	c.solved = make(map[solutionKey[T, P]][]*Match[T, P])

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
	return c.solved != nil
}

// Index implements Memo
func (c *ParseContext[T, P]) Index(pos P) int {
	return c.index(pos)
}

// Solutions implements Memo
func (c *ParseContext[T, P]) Solutions(p Pattern[T, P], pos P) ([]*Match[T, P], bool) {
	m, ok := c.solved[solutionKey[T, P]{p, c.index(pos)}]
	return m, ok
}

// SetSolutions implements Memo
func (c *ParseContext[T, P]) SetSolutions(p Pattern[T, P], pos P, matches []*Match[T, P]) {
	c.solved[solutionKey[T, P]{p, c.index(pos)}] = matches
}

// LogMismatch reports a mismatch of pattern p to the logger of the reader, if it is a ParseContext with a logger. The
// mismatch is only built if somebody is listening, which saves an allocation for every failed alternative when
// nothing is logged.
func LogMismatch[T, P any](r Reader[T, P], p Pattern[T, P], build func() *Mismatch[T, P]) {
	c, ok := r.(interface{ Logger() Logger[T, P] })
	if !ok {
		return
	}

	logger := c.Logger()
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
	quiet.solved = nil

	return &quiet
}
