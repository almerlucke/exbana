package main

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

var modes = []Mode{Backtracking, Memoized, Committed}

func newCalc(mode Mode) *Calculator {
	c := NewCalculator(mode)
	c.Vars["x"] = 5
	c.Vars["y"] = 2
	return c
}

func TestEval(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"1", 1},
		{"1+2*3", 7},
		{"(1+2)*3", 9},
		{" 1 +\t2 * 3 ", 7},
		{"10-4-3", 3},   // left associative
		{"100/10/5", 2}, // left associative
		{"2^3^2", 512},  // right associative
		{"-2^2", -4},    // unary binds weaker than ^
		{"2^-1", 0.5},   // unary in exponent
		{"2*-3", -6},    // unary after operator
		{"- - 3", 3},    // stacked unary
		{"-(-3)", 3},    // unary on parenthesis
		{"+4", 4},       // unary plus
		{"10%4", 2},     // modulo
		{"1.5e2+0.5", 150.5},
		{"2e-1*10", 2},
		{"x*y", 10},
		{"max(1, 2*3, 4)", 6},
		{"min(x, y)", 2},
		{"sqrt(16)+abs(-3)", 7},
		{"max(sqrt(16), min(9, abs(-7)))", 7},
		{"((((((1+2))))))*2", 6},
		{"1 - 2 - 3 - 4", -8},
		{"2*3^2*2", 36},
	}

	for _, mode := range modes {
		c := newCalc(mode)
		for _, tc := range cases {
			got, err := c.Eval(tc.in)
			if err != nil || math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("%v: %q = %v, %v; want %v", mode, tc.in, got, err, tc.want)
			}
		}
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1+", `syntax error at 1:3: unexpected end of input, expected sign, digit, letter or "("`},
		{"1+*2", `syntax error at 1:3: unexpected '*', expected sign, digit, letter or "("`},
		{"(1+2", `syntax error at 1:5: unexpected end of input, expected digit, ".", exponent, "^", multiplication operator, addition operator or ")"`},
		{"1 2", `unexpected '2'`},
		{"", `syntax error at 1:1: unexpected end of input`},
		{"foo(1)", `undefined function "foo"`},
		{"z+1", `undefined variable "z"`},
		{"max()", `max needs at least one argument`},
		{"1+2)", `unexpected ')'`},
	}

	for _, mode := range modes {
		c := newCalc(mode)
		for _, tc := range cases {
			_, err := c.Eval(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%v: %q: got %v, want %q", mode, tc.in, err, tc.want)
			}
		}
	}
}

func TestErrorPositionMultiLine(t *testing.T) {
	for _, mode := range modes {
		_, err := newCalc(mode).Eval("1 +\n 2 *\n 3 + )")
		if err == nil || !strings.Contains(err.Error(), "at 3:6: unexpected ')'") {
			t.Errorf("%v: got %v", mode, err)
		}
	}
}

// flat builds 1+2*3-4/5+... with n terms
func flat(n int) string {
	var sb strings.Builder
	ops := []string{"+", "*", "-", "/"}
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(" " + ops[i%len(ops)] + " ")
		}
		sb.WriteString(fmt.Sprint(i%9 + 1))
	}
	return sb.String()
}

func nested(depth int, tail string) string {
	return strings.Repeat("(", depth) + tail + strings.Repeat(")", depth)
}

func TestModesAgree(t *testing.T) {
	in := flat(300)
	var want float64
	for i, mode := range modes {
		got, err := newCalc(mode).Eval(in)
		if err != nil {
			t.Fatalf("%v: %v", mode, err)
		}
		if i == 0 {
			want = got
		} else if math.Abs(got-want) > 1e-9 {
			t.Errorf("%v gives %v, backtracking gives %v", mode, got, want)
		}
	}
}

// Input that fails late forces the backtracking modes to try every way of giving back repetitions.
// This must stay fast with memoization and with committed matching.
func TestLateFailureIsFast(t *testing.T) {
	for _, mode := range []Mode{Memoized, Committed} {
		c := newCalc(mode)
		start := time.Now()
		_, err := c.Eval(nested(40, "1+2*3-")) // unbalanced, fails after a lot of work
		if err == nil {
			t.Errorf("%v: expected an error", mode)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%v: took %v", mode, d)
		}
	}
}

func BenchmarkFlat(b *testing.B) {
	in := flat(2000)
	for _, mode := range modes {
		c := newCalc(mode)
		b.Run(mode.String(), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := c.Eval(in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkNested(b *testing.B) {
	in := nested(100, "1+2*3")
	for _, mode := range modes {
		c := newCalc(mode)
		b.Run(mode.String(), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := c.Eval(in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFailNested(b *testing.B) {
	for _, depth := range []int{4, 8, 12} {
		in := nested(depth, "1+2*3-")
		for _, mode := range modes {
			c := newCalc(mode)
			b.Run(fmt.Sprintf("depth%d/%v", depth, mode), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					if _, err := c.Eval(in); err == nil {
						b.Fatal("expected error")
					}
				}
			})
		}
	}
}

// The grammar is shared by all parses and never changed by one, so concurrent Evals must not interfere or race.
func TestConcurrentEval(t *testing.T) {
	inputs := []struct {
		in   string
		want string // "" for success
	}{
		{"1+2*3", ""},
		{"max(1, x*2, 3)", ""},
		{"1+", "at 1:3"},
		{"(1+2", "at 1:5"},
		{"2^3^2", ""},
		{"1 + * 2", "at 1:5"},
	}

	for _, mode := range modes {
		c := newCalc(mode)
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 200; i++ {
					tc := inputs[(g+i)%len(inputs)]
					_, err := c.Eval(tc.in)
					switch {
					case tc.want == "" && err != nil:
						t.Errorf("%v: %q: unexpected error %v", mode, tc.in, err)
					case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
						t.Errorf("%v: %q: got %v, want error containing %q", mode, tc.in, err, tc.want)
					}
				}
			}()
		}
		wg.Wait()
	}
}
