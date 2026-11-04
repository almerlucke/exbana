package exbana

import (
	"errors"
	"runtime/metrics"
)

// ErrTooDeep is returned when matching has recursed so deeply that the goroutine stack is close to the limit of the Go
// runtime. Running into that limit is a fatal error that can not be recovered from, so the matcher reports it as an
// error first. A grammar gets here with a very long repetition that is not possessive, or input nested ten thousands of
// levels deep. Possessive repetitions (SetPossessive) and the memoizing reader (ParseContext.WithMemo) match a repetition
// in a loop and do not have the problem.
var ErrTooDeep = errors.New("exbana: matching is nested too deeply, use a possessive repetition or WithMemo for long repetitions")

// stackLimit is the size at which a stack is given up on. The Go runtime stops a goroutine whose stack would grow
// beyond 1 GB, and stacks only come in powers of two, so the biggest a stack can be is 512 MiB. A stack that has
// grown to that size is using more than half of it, and the next doubling is the fatal one.
const stackLimit = 512 << 20

// checkEvery is how often CheckStack looks at the runtime: reading the metric costs a little more than a recursion step.
const checkEvery = 4096

// CheckStack returns ErrTooDeep if the stack is close to the limit. Patterns that recurse once for every item of a long
// input call it with the depth they are at, and it only looks at the runtime every few thousand levels. The stack is
// measured for the whole process, so this is conservative if many goroutines have big stacks, and it assumes the default
// stack limit of the Go runtime.
func CheckStack(depth int) error {
	if depth%checkEvery != 0 || depth == 0 {
		return nil
	}

	sample := []metrics.Sample{{Name: "/memory/classes/heap/stacks:bytes"}}

	metrics.Read(sample)

	if sample[0].Value.Kind() == metrics.KindUint64 && sample[0].Value.Uint64() >= stackLimit {
		return ErrTooDeep
	}

	return nil
}
