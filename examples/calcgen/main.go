// Command calcgen evaluates arithmetic with a parser that was generated from a grammar with left recursive rules.
//
//	go run ./examples/calcgen '10 - 3 - 2 * (4 + 1)'
//
// The parser is in the parser package; regenerate it with go generate after changing the grammar.
package main

//go:generate go run ./gen

import (
	"fmt"
	"math"
	"os"
	"strconv"

	"github.com/almerlucke/exbana/v3/examples/calcgen/parser"
)

func main() {
	input := "1 + 2 * 3 - 4 / (1 + 1)"
	if len(os.Args) > 1 {
		input = os.Args[1]
	}

	v, err := Eval(input)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println(v)
}

// Eval parses the input and evaluates the tree.
func Eval(input string) (float64, error) {
	tree, err := parser.NewCalcParser().Parse(input)
	if err != nil {
		return 0, err
	}

	input0, _ := tree.FirstExpr(0)

	return eval(tree, input0)
}

// eval walks the tree. A left recursive rule is one node with its parts as children, in order: a term, an operator, a term,
// an operator, and so on, which is the order to evaluate them in, so that the operators group to the left.
func eval(tree *parser.CalcTree, node int) (float64, error) {
	switch tree.Rule(node) {
	case parser.CalcRuleNumber:
		return strconv.ParseFloat(trim(tree.Text(node)), 64)
	case parser.CalcRulePrimary:
		if n, ok := tree.FirstNumber(node); ok {
			return eval(tree, n)
		}

		e, _ := tree.FirstExpr(node)

		return eval(tree, e)
	case parser.CalcRuleUnary:
		if p, ok := tree.FirstPrimary(node); ok {
			return eval(tree, p)
		}

		u, _ := tree.FirstUnary(node)

		v, err := eval(tree, u)

		return -v, err
	case parser.CalcRuleExpr, parser.CalcRuleTerm:
		var parts []int
		for c := range tree.Children(node) {
			parts = append(parts, c)
		}

		v, err := eval(tree, parts[0])
		if err != nil {
			return 0, err
		}

		for i := 1; i+1 < len(parts); i += 2 {
			r, err := eval(tree, parts[i+1])
			if err != nil {
				return 0, err
			}

			switch op := trim(tree.Text(parts[i])); op {
			case "+":
				v += r
			case "-":
				v -= r
			case "*":
				v *= r
			case "/":
				if r == 0 {
					return 0, fmt.Errorf("division by zero")
				}

				v /= r
			case "%":
				v = math.Mod(v, r)
			}
		}

		return v, nil
	}

	return 0, fmt.Errorf("rule %v", tree.Rule(node))
}

// trim takes the white space after a token off
func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}

	return s
}
