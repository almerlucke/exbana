package run

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
	"github.com/almerlucke/exbana/v3/examples/covenant/check"
)

// rtime is what the library needs of the way a program is run: to call a closure, to say where a fault is, and what the program is.
// The tree walker and the bytecode machine both are one, so they share the library.
type rtime interface {
	program() *Program
	context() context.Context
	callback(f Value, at ast.Node, args ...Value) (Value, error)
	fault(at ast.Node, kind, msg string) *Fault
}

type native func(m rtime, at ast.Node, a []Value) (Value, error)

// callNative calls a function that has a signature and no body: one of the library, or one the host provides.
func (m *machine) callNative(fd *check.FnDef, args []Value, at ast.Node) (Value, error) {
	return nativeCall(m, fd, args, at)
}

func nativeCall(rt rtime, fd *check.FnDef, args []Value, at ast.Node) (Value, error) {
	name := fd.Name
	if fd.Owner != nil {
		name = fd.Owner.Name + "." + fd.Name
	}

	if fd.Module.Name == "prelude" {
		if f, ok := stdlib[name]; ok {
			return f(rt, at, args)
		}

		return nil, rt.fault(at, "internal", "the library has no "+name)
	}

	key := name
	if fd.Owner == nil {
		key = fd.Module.Name + "::" + fd.Name
	}

	h, ok := rt.program().cfg.Host[key]
	if !ok {
		return nil, rt.fault(at, "host", "the host does not provide "+key)
	}

	v, err := h(rt.context(), args)
	if err != nil {
		var f *Fault
		var l *Limit

		if errors.As(err, &f) || errors.As(err, &l) {
			return nil, err
		}

		return nil, rt.fault(at, "host", key+": "+err.Error())
	}

	return v, nil
}

// call calls a function value from the library.
func (m *machine) callback(f Value, at ast.Node, args ...Value) (Value, error) {
	return m.callFunc(f.(*Func), args, at)
}

func (m *machine) program() *Program        { return m.p }
func (m *machine) context() context.Context { return m.ctx }

func enumOf(m rtime, enum, variant string, fields ...Value) Value {
	return m.program().variant(enum, variant, fields...)
}

func someOf(m rtime, v Value) Value { return enumOf(m, "Option", "Some", v) }
func noneOf(m rtime) Value          { return enumOf(m, "Option", "None") }

func rangeFault(m rtime, at ast.Node, msg string) error { return m.fault(at, "range", msg) }

// isVariant says whether an enum value is of the variant.
func isVariant(v Value, name string) bool { return v.(*Enum).Variant.Name == name }

var stdlib map[string]native

func init() {
	stdlib = map[string]native{
		"range": func(m rtime, at ast.Node, a []Value) (Value, error) {
			from, to := a[0].(int64), a[1].(int64)
			if to <= from {
				return NewList(nil), nil
			}

			if to-from > 50_000_000 || to-from < 0 {
				return nil, rangeFault(m, at, "range is too long to make")
			}

			items := make([]Value, 0, to-from)
			for i := from; i < to; i++ {
				items = append(items, i)
			}

			return &List{&listBase{items}, len(items)}, nil
		},

		// Int
		"Int.to_float": func(m rtime, at ast.Node, a []Value) (Value, error) { return float64(a[0].(int64)), nil },
		"Int.to_text": func(m rtime, at ast.Node, a []Value) (Value, error) {
			return strconv.FormatInt(a[0].(int64), 10), nil
		},
		"Int.abs": func(m rtime, at ast.Node, a []Value) (Value, error) {
			n := a[0].(int64)
			if n == math.MinInt64 {
				return nil, m.fault(at, "overflow", "the absolute value of "+strconv.FormatInt(n, 10)+" does not fit in an Int")
			}

			if n < 0 {
				return -n, nil
			}

			return n, nil
		},
		"Int.min": func(m rtime, at ast.Node, a []Value) (Value, error) { return min(a[0].(int64), a[1].(int64)), nil },
		"Int.max": func(m rtime, at ast.Node, a []Value) (Value, error) { return max(a[0].(int64), a[1].(int64)), nil },

		// Float
		"Float.to_text": func(m rtime, at ast.Node, a []Value) (Value, error) { return formatFloat(a[0].(float64)), nil },
		"Float.abs":     func(m rtime, at ast.Node, a []Value) (Value, error) { return math.Abs(a[0].(float64)), nil },
		"Float.min": func(m rtime, at ast.Node, a []Value) (Value, error) {
			return math.Min(a[0].(float64), a[1].(float64)), nil
		},
		"Float.max": func(m rtime, at ast.Node, a []Value) (Value, error) {
			return math.Max(a[0].(float64), a[1].(float64)), nil
		},
		"Float.sqrt": func(m rtime, at ast.Node, a []Value) (Value, error) { return math.Sqrt(a[0].(float64)), nil },
		"Float.floor": func(m rtime, at ast.Node, a []Value) (Value, error) {
			return toIntOf(m, at, math.Floor(a[0].(float64)))
		},
		"Float.round": func(m rtime, at ast.Node, a []Value) (Value, error) {
			return toIntOf(m, at, math.Round(a[0].(float64)))
		},

		// Bool
		"Bool.to_text": func(m rtime, at ast.Node, a []Value) (Value, error) { return strconv.FormatBool(a[0].(bool)), nil },

		// Text
		"Text.length": func(m rtime, at ast.Node, a []Value) (Value, error) {
			return int64(utf8.RuneCountInString(a[0].(string))), nil
		},
		"Text.is_empty": func(m rtime, at ast.Node, a []Value) (Value, error) { return a[0].(string) == "", nil },
		"Text.contains": func(m rtime, at ast.Node, a []Value) (Value, error) {
			return strings.Contains(a[0].(string), a[1].(string)), nil
		},
		"Text.starts_with": func(m rtime, at ast.Node, a []Value) (Value, error) {
			return strings.HasPrefix(a[0].(string), a[1].(string)), nil
		},
		"Text.trim":  func(m rtime, at ast.Node, a []Value) (Value, error) { return strings.TrimSpace(a[0].(string)), nil },
		"Text.upper": func(m rtime, at ast.Node, a []Value) (Value, error) { return strings.ToUpper(a[0].(string)), nil },
		"Text.lower": func(m rtime, at ast.Node, a []Value) (Value, error) { return strings.ToLower(a[0].(string)), nil },
		"Text.split": func(m rtime, at ast.Node, a []Value) (Value, error) {
			var parts []string

			if a[1].(string) == "" {
				for _, r := range a[0].(string) {
					parts = append(parts, string(r))
				}
			} else {
				parts = strings.Split(a[0].(string), a[1].(string))
			}

			items := make([]Value, len(parts))
			for i, p := range parts {
				items[i] = p
			}

			return &List{&listBase{items}, len(items)}, nil
		},
		"Text.to_int": func(m rtime, at ast.Node, a []Value) (Value, error) {
			s := a[0].(string)
			if s == "" || strings.ContainsAny(s, "_ \t\n") || strings.HasPrefix(strings.TrimLeft(s, "+-"), "0x") {
				return parseErrorOf(m, "NotANumber"), nil
			}

			n, err := strconv.ParseInt(s, 10, 64)

			switch {
			case errors.Is(err, strconv.ErrRange):
				return parseErrorOf(m, "OutOfRange"), nil
			case err != nil:
				return parseErrorOf(m, "NotANumber"), nil
			}

			return enumOf(m, "Result", "Ok", n), nil
		},
		"Text.to_float": func(m rtime, at ast.Node, a []Value) (Value, error) {
			s := a[0].(string)
			if s == "" || strings.Trim(s, "0123456789+-.eE") != "" {
				return parseErrorOf(m, "NotANumber"), nil
			}

			f, err := strconv.ParseFloat(s, 64)

			switch {
			case errors.Is(err, strconv.ErrRange):
				return parseErrorOf(m, "OutOfRange"), nil
			case err != nil:
				return parseErrorOf(m, "NotANumber"), nil
			}

			return enumOf(m, "Result", "Ok", f), nil
		},

		// List
		"List.length":   func(m rtime, at ast.Node, a []Value) (Value, error) { return int64(a[0].(*List).Len()), nil },
		"List.is_empty": func(m rtime, at ast.Node, a []Value) (Value, error) { return a[0].(*List).Len() == 0, nil },
		"List.get": func(m rtime, at ast.Node, a []Value) (Value, error) {
			l, i := a[0].(*List), a[1].(int64)
			if i < 0 || i >= int64(l.Len()) {
				return noneOf(m), nil
			}

			return someOf(m, l.At(int(i))), nil
		},
		"List.first": func(m rtime, at ast.Node, a []Value) (Value, error) {
			l := a[0].(*List)
			if l.Len() == 0 {
				return noneOf(m), nil
			}

			return someOf(m, l.At(0)), nil
		},
		"List.last": func(m rtime, at ast.Node, a []Value) (Value, error) {
			l := a[0].(*List)
			if l.Len() == 0 {
				return noneOf(m), nil
			}

			return someOf(m, l.At(l.Len()-1)), nil
		},
		"List.push": func(m rtime, at ast.Node, a []Value) (Value, error) { return a[0].(*List).Push(a[1]), nil },
		"List.concat": func(m rtime, at ast.Node, a []Value) (Value, error) {
			return NewList(append(append([]Value(nil), a[0].(*List).Items()...), a[1].(*List).Items()...)), nil
		},
		"List.reverse": func(m rtime, at ast.Node, a []Value) (Value, error) {
			items := a[0].(*List).Items()
			out := make([]Value, len(items))

			for i, x := range items {
				out[len(items)-1-i] = x
			}

			return &List{&listBase{out}, len(out)}, nil
		},
		"List.map": func(m rtime, at ast.Node, a []Value) (Value, error) {
			items := a[0].(*List).Items()
			out := make([]Value, len(items))

			for i, x := range items {
				v, err := m.callback(a[1], at, x)
				if err != nil {
					return nil, err
				}

				out[i] = v
			}

			return &List{&listBase{out}, len(out)}, nil
		},
		"List.filter": func(m rtime, at ast.Node, a []Value) (Value, error) {
			var out []Value

			for _, x := range a[0].(*List).Items() {
				v, err := m.callback(a[1], at, x)
				if err != nil {
					return nil, err
				}

				if v == true {
					out = append(out, x)
				}
			}

			return &List{&listBase{out}, len(out)}, nil
		},
		"List.fold": func(m rtime, at ast.Node, a []Value) (Value, error) {
			acc := a[1]

			for _, x := range a[0].(*List).Items() {
				v, err := m.callback(a[2], at, acc, x)
				if err != nil {
					return nil, err
				}

				acc = v
			}

			return acc, nil
		},
		"List.any": func(m rtime, at ast.Node, a []Value) (Value, error) {
			for _, x := range a[0].(*List).Items() {
				v, err := m.callback(a[1], at, x)
				if err != nil {
					return nil, err
				}

				if v == true {
					return true, nil
				}
			}

			return false, nil
		},
		"List.all": func(m rtime, at ast.Node, a []Value) (Value, error) {
			for _, x := range a[0].(*List).Items() {
				v, err := m.callback(a[1], at, x)
				if err != nil {
					return nil, err
				}

				if v != true {
					return false, nil
				}
			}

			return true, nil
		},

		// Map
		"Map.length": func(m rtime, at ast.Node, a []Value) (Value, error) { return int64(a[0].(*Map).Len()), nil },
		"Map.get": func(m rtime, at ast.Node, a []Value) (Value, error) {
			if v, ok := a[0].(*Map).Get(a[1]); ok {
				return someOf(m, v), nil
			}

			return noneOf(m), nil
		},
		"Map.has": func(m rtime, at ast.Node, a []Value) (Value, error) {
			_, ok := a[0].(*Map).Get(a[1])
			return ok, nil
		},
		"Map.set":    func(m rtime, at ast.Node, a []Value) (Value, error) { return a[0].(*Map).Set(a[1], a[2]), nil },
		"Map.remove": func(m rtime, at ast.Node, a []Value) (Value, error) { return a[0].(*Map).Remove(a[1]), nil },
		"Map.keys": func(m rtime, at ast.Node, a []Value) (Value, error) {
			mp := a[0].(*Map)
			out := make([]Value, mp.Len())

			for i := range out {
				out[i], _ = mp.Entry(i)
			}

			return &List{&listBase{out}, len(out)}, nil
		},
		"Map.values": func(m rtime, at ast.Node, a []Value) (Value, error) {
			mp := a[0].(*Map)
			out := make([]Value, mp.Len())

			for i := range out {
				_, out[i] = mp.Entry(i)
			}

			return &List{&listBase{out}, len(out)}, nil
		},

		// Set
		"Set.from": func(m rtime, at ast.Node, a []Value) (Value, error) {
			s := NewSet()
			for _, x := range a[0].(*List).Items() {
				s = s.Add(x)
			}

			return s, nil
		},
		"Set.length": func(m rtime, at ast.Node, a []Value) (Value, error) { return int64(a[0].(*Set).Len()), nil },
		"Set.has":    func(m rtime, at ast.Node, a []Value) (Value, error) { return a[0].(*Set).Has(a[1]), nil },
		"Set.add":    func(m rtime, at ast.Node, a []Value) (Value, error) { return a[0].(*Set).Add(a[1]), nil },
		"Set.remove": func(m rtime, at ast.Node, a []Value) (Value, error) { return a[0].(*Set).Remove(a[1]), nil },
		"Set.to_list": func(m rtime, at ast.Node, a []Value) (Value, error) {
			s := a[0].(*Set)
			out := make([]Value, s.Len())

			for i := range out {
				out[i] = s.Item(i)
			}

			return &List{&listBase{out}, len(out)}, nil
		},

		// Option
		"Option.is_some": func(m rtime, at ast.Node, a []Value) (Value, error) { return isVariant(a[0], "Some"), nil },
		"Option.is_none": func(m rtime, at ast.Node, a []Value) (Value, error) { return isVariant(a[0], "None"), nil },
		"Option.unwrap_or": func(m rtime, at ast.Node, a []Value) (Value, error) {
			if e := a[0].(*Enum); e.Variant.Name == "Some" {
				return e.Fields[0], nil
			}

			return a[1], nil
		},
		"Option.or_error": func(m rtime, at ast.Node, a []Value) (Value, error) {
			if e := a[0].(*Enum); e.Variant.Name == "Some" {
				return enumOf(m, "Result", "Ok", e.Fields[0]), nil
			}

			return enumOf(m, "Result", "Err", a[1]), nil
		},
		"Option.map": func(m rtime, at ast.Node, a []Value) (Value, error) {
			e := a[0].(*Enum)
			if e.Variant.Name != "Some" {
				return e, nil
			}

			v, err := m.callback(a[1], at, e.Fields[0])
			if err != nil {
				return nil, err
			}

			return someOf(m, v), nil
		},

		// Result
		"Result.is_ok":  func(m rtime, at ast.Node, a []Value) (Value, error) { return isVariant(a[0], "Ok"), nil },
		"Result.is_err": func(m rtime, at ast.Node, a []Value) (Value, error) { return isVariant(a[0], "Err"), nil },
		"Result.unwrap_or": func(m rtime, at ast.Node, a []Value) (Value, error) {
			if e := a[0].(*Enum); e.Variant.Name == "Ok" {
				return e.Fields[0], nil
			}

			return a[1], nil
		},
		"Result.map_error": func(m rtime, at ast.Node, a []Value) (Value, error) {
			e := a[0].(*Enum)
			if e.Variant.Name == "Ok" {
				return e, nil
			}

			v, err := m.callback(a[1], at, e.Fields[0])
			if err != nil {
				return nil, err
			}

			return enumOf(m, "Result", "Err", v), nil
		},
		"Result.ok": func(m rtime, at ast.Node, a []Value) (Value, error) {
			if e := a[0].(*Enum); e.Variant.Name == "Ok" {
				return someOf(m, e.Fields[0]), nil
			}

			return noneOf(m), nil
		},
	}

	for name, f := range dataLibrary() {
		stdlib[name] = f
	}
}

// toIntOf turns a Float with no fraction into an Int, and it is a fault if it does not fit.
func toIntOf(m rtime, at ast.Node, f float64) (Value, error) {
	if math.IsNaN(f) || f >= 9223372036854775808.0 || f < -9223372036854775808.0 {
		return nil, m.fault(at, "range", formatFloat(f)+" does not fit in an Int")
	}

	return int64(f), nil
}

func parseErrorOf(m rtime, variant string) Value {
	def := m.program().defs["ParseError"]
	return enumOf(m, "Result", "Err", &Enum{def, variantOf(def, variant), nil})
}
