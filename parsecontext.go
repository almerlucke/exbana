package exbana

import "slices"

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

	// frames that are done, kept to be used again: a rule call would otherwise allocate a frame every time
	free []*RuleFrame[T, P]

	// the matches of left recursive rules, see RuleResult
	results map[Pattern[T, P]][]ruleResult[T, P]
}

type ruleResult[T, P any] struct {
	pos     P
	matches []*Match[T, P]
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
	var f *RuleFrame[T, P]

	if n := len(c.frames.free); n > 0 {
		f = c.frames.free[n-1]
		c.frames.free = c.frames.free[:n-1]
	} else {
		f = &RuleFrame[T, P]{}
	}

	*f = RuleFrame[T, P]{pattern: p, pos: pos, Open: true, Grows: grows}
	c.frames.frames = append(c.frames.frames, f)

	return f
}

// FrameDepth returns the number of rules that are being matched, which is how deeply rules are nested at this point.
func (c *ParseContext[T, P]) FrameDepth() int {
	return len(c.frames.frames)
}

// PopFrame removes the frame, and any frames pushed after it. The frame must not be used after it was popped.
func (c *ParseContext[T, P]) PopFrame(f *RuleFrame[T, P]) {
	s := c.frames.frames

	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == f {
			clear(s[i:])
			c.frames.frames = s[:i]

			// only the frame itself is reused; frames above it were not popped by their own rule, so something may
			// still refer to them
			*f = RuleFrame[T, P]{}
			c.frames.free = append(c.frames.free, f)

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

// RuleResult returns the matches a left recursive rule has found at the position earlier in this parse. A rule that is
// used at many positions, like an expression in the parentheses of an expression, would otherwise grow its seed again
// every time it is reached, and nested use makes that exponential. It is independent of WithMemo.
// While a rule is growing a seed at the position nothing is known: what is found there depends on the seed.
func (c *ParseContext[T, P]) RuleResult(p Pattern[T, P], pos P) ([]*Match[T, P], bool) {
	if c.growingAt(pos) {
		return nil, false
	}

	entries := c.frames.results[p]

	i, found := slices.BinarySearchFunc(entries, pos, func(e ruleResult[T, P], target P) int {
		// negative when e is before target
		return -c.Length(e.pos, target)
	})
	if !found {
		return nil, false
	}

	return entries[i].matches, true
}

// SetRuleResult remembers the matches of a left recursive rule at the position, see RuleResult. It is ignored while a
// rule is growing a seed at the position.
func (c *ParseContext[T, P]) SetRuleResult(p Pattern[T, P], pos P, matches []*Match[T, P]) {
	if c.growingAt(pos) {
		return
	}

	if c.frames.results == nil {
		c.frames.results = make(map[Pattern[T, P]][]ruleResult[T, P])
	}

	entries := c.frames.results[p]

	i, found := slices.BinarySearchFunc(entries, pos, func(e ruleResult[T, P], target P) int {
		return -c.Length(e.pos, target)
	})
	if found {
		entries[i].matches = matches
		return
	}

	c.frames.results[p] = slices.Insert(entries, i, ruleResult[T, P]{pos: pos, matches: matches})
}

// growingAt returns true if a rule is growing a seed at the position. Results of patterns at that position depend on
// the seed, which changes while it grows, so they must not be memoized.
func (c *ParseContext[T, P]) growingAt(pos P) bool {
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

func (c *ParseContext[T, P]) memoEnabled() bool {
	return c.solved != nil
}

func (c *ParseContext[T, P]) indexOf(pos P) int {
	return c.index(pos)
}

func (c *ParseContext[T, P]) solutions(p Pattern[T, P], pos P) ([]*Match[T, P], bool) {
	m, ok := c.solved[solutionKey[T, P]{p, c.index(pos)}]
	return m, ok
}

func (c *ParseContext[T, P]) setSolutions(p Pattern[T, P], pos P, matches []*Match[T, P]) {
	c.solved[solutionKey[T, P]{p, c.index(pos)}] = matches
}

// listener returns the logger of the reader if one is listening for the pattern: the reader is a ParseContext with a
// logger, which is not void and does not filter the pattern out.
func listener[T, P any](r Reader[T, P], p Pattern[T, P]) Logger[T, P] {
	c, ok := r.(interface{ Logger() Logger[T, P] })
	if !ok {
		return nil
	}

	logger := c.Logger()
	if logger == nil {
		return nil
	}

	if _, void := logger.(*voidLog[T, P]); void {
		return nil
	}

	// a logger that is not interested in this pattern saves the work of describing the mismatch
	if f, ok := logger.(MismatchFilter[T, P]); ok && !f.WantsMismatch(p) {
		return nil
	}

	return logger
}

// LogMismatch reports a mismatch of pattern p to the logger of the reader, if it is a ParseContext with a logger. The
// mismatch is only built if somebody is listening, which saves an allocation for every failed alternative when
// nothing is logged.
func LogMismatch[T, P any](r Reader[T, P], p Pattern[T, P], build func() *Mismatch[T, P]) {
	if logger := listener(r, p); logger != nil {
		logger.LogMismatch(build())
	}
}

// LogFailure reports that pattern p failed to match without saying anything more about it, the way leaf patterns fail.
// A logger that implements FailureLogger is told without a Mismatch being built, which spares an allocation for every
// failed leaf of a parse; any other logger gets the Mismatch from begin to end.
func LogFailure[T, P any](r Reader[T, P], p Pattern[T, P], begin, end P) {
	logger := listener(r, p)
	if logger == nil {
		return
	}

	if f, ok := logger.(FailureLogger[T, P]); ok {
		f.LogFailure(p, begin)
		return
	}

	logger.LogMismatch(NewMismatch(p, begin, end, nil, nil))
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
	quiet.logger = &voidLog[T, P]{}
	quiet.index = nil
	quiet.solved = nil

	return &quiet
}
