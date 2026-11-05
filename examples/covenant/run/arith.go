package run

import (
	"math"
	"strconv"
)

// binaryOp applies an arithmetic or a comparison operator to two values of the same type. It is what both ways of running a
// program use, so they agree. If it can not, kind says what went wrong (a fault) and msg says it; kind is "" when it worked.
func binaryOp(op string, l, r Value) (v Value, kind, msg string) {
	switch a := l.(type) {
	case int64:
		return intOp(op, a, r.(int64))
	case float64:
		return floatOp(op, a, r.(float64))
	case string:
		b := r.(string)

		switch op {
		case "+":
			return a + b, "", ""
		case "<":
			return a < b, "", ""
		case "<=":
			return a <= b, "", ""
		case ">":
			return a > b, "", ""
		case ">=":
			return a >= b, "", ""
		}
	}

	return nil, "internal", "an operator on values it does not fit"
}

func floatOp(op string, a, b float64) (Value, string, string) {
	switch op {
	case "+":
		return a + b, "", ""
	case "-":
		return a - b, "", ""
	case "*":
		return a * b, "", ""
	case "/":
		return a / b, "", ""
	case "<":
		return a < b, "", ""
	case "<=":
		return a <= b, "", ""
	case ">":
		return a > b, "", ""
	case ">=":
		return a >= b, "", ""
	}

	return nil, "internal", "an operator on Floats it does not fit"
}

func intOp(op string, a, b int64) (Value, string, string) {
	over := func() (Value, string, string) {
		return nil, "overflow", strconv.FormatInt(a, 10) + " " + op + " " + strconv.FormatInt(b, 10) + " does not fit in an Int"
	}

	switch op {
	case "+":
		c := a + b
		if (a^c)&(b^c) < 0 {
			return over()
		}

		return c, "", ""
	case "-":
		c := a - b
		if (a^b)&(a^c) < 0 {
			return over()
		}

		return c, "", ""
	case "*":
		if a == 0 || b == 0 {
			return int64(0), "", ""
		}

		c := a * b
		if c/b != a || (a == -1 && b == math.MinInt64) || (b == -1 && a == math.MinInt64) {
			return over()
		}

		return c, "", ""
	case "/", "%":
		if b == 0 {
			return nil, "division", strconv.FormatInt(a, 10) + " " + op + " 0"
		}

		if a == math.MinInt64 && b == -1 {
			if op == "%" {
				return int64(0), "", ""
			}

			return over()
		}

		if op == "/" {
			return a / b, "", ""
		}

		return a % b, "", ""
	case "<":
		return a < b, "", ""
	case "<=":
		return a <= b, "", ""
	case ">":
		return a > b, "", ""
	case ">=":
		return a >= b, "", ""
	}

	return nil, "internal", "an operator on Ints it does not fit"
}

// negate is -x.
func negate(x Value) (Value, string, string) {
	switch x := x.(type) {
	case int64:
		if x == math.MinInt64 {
			return nil, "overflow", "-" + strconv.FormatInt(x, 10) + " does not fit in an Int"
		}

		return -x, "", ""
	case float64:
		return -x, "", ""
	}

	return nil, "internal", "an operator on a value it does not fit"
}
