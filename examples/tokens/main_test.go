package main

import (
	"strings"
	"sync"
	"testing"
)

func TestEval(t *testing.T) {
	calc := NewCalculator()

	for input, want := range map[string]float64{
		"7":                 7,
		"1 + 2":             3,
		"1 + 2 * 3":         7,
		"(1 + 2) * 3":       9,
		"10 - 3 - 2":        5, // left associative: (10-3)-2, not 10-(3-2)
		"100 / 10 / 5":      2, // (100/10)/5
		"2 * 3 + 4 * 5":     26,
		"1 - 2 - 3 - 4":     -8,
		"((((1))))":         1,
		"2 * (3 + (4 - 1))": 12,
		"0.5 + 0.25":        0.75,
		"  1+2  *  3  ":     7,
		"8 / 4 / 2 * 3 - 1": 2,
	} {
		got, err := calc.Eval(input)
		if err != nil || got != want {
			t.Errorf("%q = %v, %v, want %v", input, got, err, want)
		}
	}
}

func TestErrors(t *testing.T) {
	calc := NewCalculator()

	for input, want := range map[string]string{
		"":          "unexpected end of input",
		"1 +":       `unexpected end of input, expected number or "("`,
		"2 * (3 + ": `expected number or "("`,
		"(1 + 2":    `expected "*", "/", "+", "-" or ")"`,
		"4 4":       `unexpected "4"`,
		"1 + * 2":   `unexpected "*"`,
		"1 / 0":     "division by zero",
		"2 $ 3":     `unexpected '$' at 2`,
		"1.2.3":     "bad number",
		")":         `unexpected ")"`,
	} {
		v, err := calc.Eval(input)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q = %v, %v, want an error containing %q", input, v, err, want)
		}
	}
}

func TestErrorsSayWhereInTheText(t *testing.T) {
	_, err := NewCalculator().Eval("1 + 2 * * 3")
	if err == nil || !strings.Contains(err.Error(), "column 9") {
		t.Errorf("err = %v", err)
	}
}

func TestSharedBetweenGoroutines(t *testing.T) {
	calc := NewCalculator()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				if v, err := calc.Eval("(1 + 2) * 3 - 4 / 2"); err != nil || v != 7 {
					t.Errorf("got %v, %v", v, err)
					return
				}
				if _, err := calc.Eval("1 + * 2"); err == nil {
					t.Error("expected an error")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestLongChainOfOperators(t *testing.T) {
	// left recursion grows its seed once per operator, so a long expression is a fair test of the cost
	in := "1" + strings.Repeat(" + 1", 300)

	if v, err := NewCalculator().Eval(in); err != nil || v != 301 {
		t.Errorf("got %v, %v", v, err)
	}
}

func TestDeeplyNestedParentheses(t *testing.T) {
	// nesting used to multiply the work by about twenty for every level
	in := strings.Repeat("(1 + ", 30) + "1" + strings.Repeat(")", 30)

	if v, err := NewCalculator().Eval(in); err != nil || v != 31 {
		t.Errorf("got %v, %v", v, err)
	}
}
