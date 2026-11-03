package exbana

import (
	"errors"
	"fmt"
	"math/rand"
)

// DefaultMaxDepth is the generation depth limit of a new GenContext
const DefaultMaxDepth = 1000

// ErrMaxDepth is returned by Generate when a (recursive) grammar nests deeper than the depth limit of the GenContext
var ErrMaxDepth = errors.New("exbana: maximum generation depth exceeded")

// GenContext carries everything a Generate run needs besides the output writer: the source of randomness, a limit on
// how deep the generation may nest (so recursive grammars end), and free form state for callbacks.
// A GenContext is not safe for concurrent use, use one per Generate run. Generate must not be called with a nil context.
type GenContext struct {
	// Rand is the source of all randomness during generation. Seed it for reproducible output.
	Rand *rand.Rand

	// MaxDepth is the maximum nesting depth of composite patterns, Generate returns ErrMaxDepth when it is exceeded.
	// Zero means no limit, which makes generation of a recursive grammar without an escape hang or overflow the stack.
	MaxDepth int

	// State is free for use by generate functions and weight functions, for example the current key or the last note.
	State any

	depth int
}

// NewGenContext creates a context with a seeded random source and the default depth limit.
// The same seed on the same grammar gives the same output.
func NewGenContext(seed int64) *GenContext {
	return &GenContext{Rand: rand.New(rand.NewSource(seed)), MaxDepth: DefaultMaxDepth}
}

// Depth returns the current nesting depth. Weight functions can use it to steer a recursive grammar toward its end.
func (c *GenContext) Depth() int {
	return c.depth
}

// Enter must be called by composite patterns when they start generating, and Leave (usually deferred) when they are done.
// It returns ErrMaxDepth if the depth limit is exceeded, in which case Leave must not be called.
func (c *GenContext) Enter() error {
	if c.MaxDepth > 0 && c.depth >= c.MaxDepth {
		return ErrMaxDepth
	}

	c.depth++

	return nil
}

// Leave undoes a successful Enter
func (c *GenContext) Leave() {
	c.depth--
}

// Intn returns a random int in [0, n)
func (c *GenContext) Intn(n int) int {
	return c.Rand.Intn(n)
}

// Float64 returns a random float in [0.0, 1.0)
func (c *GenContext) Float64() float64 {
	return c.Rand.Float64()
}

// Choose returns an index with a probability proportional to its weight. Weights must not be negative, at least
// one must be positive.
func (c *GenContext) Choose(weights []float64) (int, error) {
	total := 0.0

	for _, w := range weights {
		if w < 0 || w != w {
			return 0, fmt.Errorf("exbana: invalid weight %v", w)
		}

		total += w
	}

	if total <= 0 {
		return 0, errors.New("exbana: weights must not all be zero")
	}

	x := c.Rand.Float64() * total

	for i, w := range weights {
		x -= w
		if x < 0 && w > 0 {
			return i, nil
		}
	}

	// rounding: fall back to the last choice that can be picked
	for i := len(weights) - 1; i >= 0; i-- {
		if weights[i] > 0 {
			return i, nil
		}
	}

	return 0, errors.New("exbana: weights must not all be zero")
}
