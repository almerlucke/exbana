package run_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/almerlucke/exbana/v3/examples/covenant/run"
)

func TestDisassembly(t *testing.T) {
	bytecode = true

	defer func() { bytecode = false }()

	p := program(t, `
fn fib(n: Int) -> Int {
    requires n >= 0
    if n < 2 {
        return n
    }
    return fib(n - 1) + fib(n - 2)
}

fn sum(items: List<Int>, k: Int) -> Int {
    return items.map(fn(x) => x + k).fold(0, fn(a, b) => a + b)
}
`, run.Config{})

	got, err := p.Disassemble("app", "fib")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"func app::fib (params 1, locals 1, captures 0)", "jmptrue", "fault      requires: requires n >= 0 is false for the call of fib",
		"calldef    func app::fib, 1 args", "add", "sub", "lt", "ret"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}

	got, err = p.Disassemble("app", "sum")
	if err != nil {
		t.Fatal(err)
	}

	// the closure takes k with it, and the one that adds does not take anything
	for _, want := range []string{"func closure (params 1, locals 1, captures 1)", "loadcap", "closure    func closure, 1 captured", "callmethod .map, 2 args"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}

	if _, err := p.Disassemble("app", "nothing"); err == nil {
		t.Error("a function that is not there")
	}
}

// Deep recursion is a stack in the machine and not in Go: a depth that would be a lot to ask of the tree is nothing.
func TestBytecodeRecursesDeep(t *testing.T) {
	bytecode = true

	defer func() { bytecode = false }()

	src := "fn down(n: Int) -> Int {\n    if n == 0 {\n        return 0\n    }\n    return 1 + down(n - 1)\n}\n"
	p := program(t, src, run.Config{MaxDepth: 200000})

	v, err := p.Call(context.Background(), "app", "down", int64(100000))
	if err != nil || v != int64(100000) {
		t.Fatalf("%v, %v", v, err)
	}
}

const fibSource = `
fn fib(n: Int) -> Int {
    if n < 2 {
        return n
    }
    return fib(n - 1) + fib(n - 2)
}

fn loop(n: Int) -> Int {
    var sum = 0
    var i = 0
    while i < n {
        sum = sum + i * 2 % 7
        i = i + 1
    }
    return sum
}

fn lists(n: Int) -> Int {
    return range(0, n).map(fn(x) => x * 2).filter(fn(x) => x % 3 == 0).fold(0, fn(a, b) => a + b)
}
`

func benchmark(b *testing.B, bc bool, fn string, arg int64) {
	bytecode = bc

	defer func() { bytecode = false }()

	p := programB(b, fibSource, run.Config{MaxSteps: -1})

	b.ResetTimer()

	for range b.N {
		if _, err := p.Call(context.Background(), "app", fn, arg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFibTree(b *testing.B)       { benchmark(b, false, "fib", 22) }
func BenchmarkFibBytecode(b *testing.B)   { benchmark(b, true, "fib", 22) }
func BenchmarkLoopTree(b *testing.B)      { benchmark(b, false, "loop", 200000) }
func BenchmarkLoopBytecode(b *testing.B)  { benchmark(b, true, "loop", 200000) }
func BenchmarkListsTree(b *testing.B)     { benchmark(b, false, "lists", 100000) }
func BenchmarkListsBytecode(b *testing.B) { benchmark(b, true, "lists", 100000) }

// The bytecode is meant to be faster. This does not time anything exactly, it only says that it is not slower by far.
func TestBytecodeIsNotSlower(t *testing.T) {
	if testing.Short() {
		t.Skip("times the two ways")
	}

	var took [2]time.Duration

	for i, bc := range []bool{false, true} {
		bytecode = bc
		p := program(t, fibSource, run.Config{MaxSteps: -1})

		start := time.Now()

		if _, err := p.Call(context.Background(), "app", "fib", int64(24)); err != nil {
			t.Fatal(err)
		}

		took[i] = time.Since(start)
	}

	bytecode = false

	t.Logf("fib(24): the tree took %v, the bytecode %v", took[0], took[1])

	if took[1] > took[0]*2 {
		t.Errorf("the bytecode is more than twice as slow as the tree")
	}
}

// A host function may keep what it was given: the arguments are its own, and not a view of the stack of the machine.
func TestHostFunctionsMayKeepTheirArguments(t *testing.T) {
	src := `
use fs
use fs::Path

fn busy(n: Int) -> Int {
    var total = 0
    var i = 0
    while i < n {
        total = total + i
        i = i + 1
    }
    return total
}

fn f() -> Int {
    let a = fs::make_path("first")
    let b = busy(50)
    let c = fs::make_path("second")
    return b + busy(20)
}
`

	for _, bc := range []bool{false, true} {
		bytecode = bc

		var kept [][]run.Value

		var prog *run.Program

		host := map[string]run.HostFunc{
			"fs::make_path": func(ctx context.Context, a []run.Value) (run.Value, error) {
				kept = append(kept, a)
				s, _ := prog.Struct("fs", "Path", nil)

				return s, nil
			},
			"fs::read_all": func(ctx context.Context, a []run.Value) (run.Value, error) { return prog.Ok(""), nil },
		}

		prog = program(t, src, run.Config{Host: host})

		if _, err := prog.Call(context.Background(), "app", "f"); err != nil {
			t.Fatal(err)
		}

		if len(kept) != 2 || kept[0][0] != "first" || kept[1][0] != "second" {
			t.Errorf("bytecode=%v: the host kept %v", bc, kept)
		}
	}

	bytecode = false
}
