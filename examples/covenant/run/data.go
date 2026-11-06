package run

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
)

// The library of Data: the cases of it are values like any other enum, made here from the name of the case.

func dataOf(m rtime, variant string, fields ...Value) Value {
	return enumOf(m, "Data", variant, fields...)
}

func dataNull(m rtime) Value { return dataOf(m, "Null") }

// maxJSONDepth is how deep a document can nest; a document that is deeper is not one.
const maxJSONDepth = 1000

// dataLibrary is the methods of Data, which the library takes in.
func dataLibrary() map[string]native {
	lib := map[string]native{}

	// the variant a Data is, and what is in it
	is := func(v Value, name string) (*Enum, bool) {
		e := v.(*Enum)
		return e, e.Variant.Name == name
	}

	option := func(m rtime, e *Enum, name string) (Value, error) {
		if e.Variant.Name == name {
			return someOf(m, e.Fields[0]), nil
		}

		return noneOf(m), nil
	}

	lib["Data.is_null"] = func(m rtime, at ast.Node, a []Value) (Value, error) {
		_, ok := is(a[0], "Null")
		return ok, nil
	}
	lib["Data.as_bool"] = func(m rtime, at ast.Node, a []Value) (Value, error) { return option(m, a[0].(*Enum), "Bool") }
	lib["Data.as_int"] = func(m rtime, at ast.Node, a []Value) (Value, error) { return option(m, a[0].(*Enum), "Int") }
	lib["Data.as_float"] = func(m rtime, at ast.Node, a []Value) (Value, error) { return option(m, a[0].(*Enum), "Float") }
	lib["Data.as_text"] = func(m rtime, at ast.Node, a []Value) (Value, error) { return option(m, a[0].(*Enum), "Text") }
	lib["Data.as_list"] = func(m rtime, at ast.Node, a []Value) (Value, error) { return option(m, a[0].(*Enum), "List") }
	lib["Data.as_map"] = func(m rtime, at ast.Node, a []Value) (Value, error) { return option(m, a[0].(*Enum), "Map") }
	lib["Data.get"] = func(m rtime, at ast.Node, a []Value) (Value, error) {
		if e, ok := is(a[0], "Map"); ok {
			if v, there := e.Fields[0].(*Map).Get(a[1]); there {
				return someOf(m, v), nil
			}
		}

		return noneOf(m), nil
	}
	lib["Data.at"] = func(m rtime, at ast.Node, a []Value) (Value, error) {
		if e, ok := is(a[0], "List"); ok {
			l, i := e.Fields[0].(*List), a[1].(int64)
			if i >= 0 && i < int64(l.Len()) {
				return someOf(m, l.At(int(i))), nil
			}
		}

		return noneOf(m), nil
	}
	lib["Data.to_json"] = func(m rtime, at ast.Node, a []Value) (Value, error) {
		var sb strings.Builder

		if err := writeJSON(&sb, a[0].(*Enum)); err != nil {
			return nil, m.fault(at, "range", err.Error())
		}

		return sb.String(), nil
	}
	lib["Data.from_json"] = func(m rtime, at ast.Node, a []Value) (Value, error) {
		v, kind := parseJSON(m, a[0].(string))
		if kind != "" {
			return parseErrorOf(m, kind), nil
		}

		return enumOf(m, "Result", "Ok", v), nil
	}

	return lib
}

// writeJSON writes a Data as JSON with nothing in it that is not needed. A Float is written so that it reads as a Float, 1.0 and
// not 1, so what is written is read back as the same value. A Float that is not a number (NaN, infinity) is not JSON.
func writeJSON(sb *strings.Builder, d *Enum) error {
	switch d.Variant.Name {
	case "Null":
		sb.WriteString("null")
	case "Bool":
		sb.WriteString(strconv.FormatBool(d.Fields[0].(bool)))
	case "Int":
		sb.WriteString(strconv.FormatInt(d.Fields[0].(int64), 10))
	case "Float":
		f := d.Fields[0].(float64)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("%s is not a number that JSON has", formatFloat(f))
		}

		out, _ := json.Marshal(f)
		sb.Write(out)

		if !bytes.ContainsAny(out, ".eE") {
			sb.WriteString(".0")
		}
	case "Text":
		sb.WriteString(quoteJSON(d.Fields[0].(string)))
	case "List":
		sb.WriteByte('[')

		for i, x := range d.Fields[0].(*List).Items() {
			if i > 0 {
				sb.WriteByte(',')
			}

			if err := writeJSON(sb, x.(*Enum)); err != nil {
				return err
			}
		}

		sb.WriteByte(']')
	case "Map":
		mp := d.Fields[0].(*Map)

		sb.WriteByte('{')

		for i := range mp.Len() {
			k, v := mp.Entry(i)
			if i > 0 {
				sb.WriteByte(',')
			}

			sb.WriteString(quoteJSON(k.(string)))
			sb.WriteByte(':')

			if err := writeJSON(sb, v.(*Enum)); err != nil {
				return err
			}
		}

		sb.WriteByte('}')
	}

	return nil
}

func quoteJSON(s string) string {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)

	return strings.TrimSuffix(buf.String(), "\n")
}

// parseJSON reads a document. kind is "" if it is one, and else the case of ParseError that says what is wrong with it: Invalid
// if it is not JSON, and OutOfRange if a number does not fit. The keys of an object are kept in the order they are written, and
// a key that is written twice keeps its first place and its last value.
func parseJSON(m rtime, text string) (Value, string) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()

	v, kind := readJSON(m, dec, 0)
	if kind != "" {
		return nil, kind
	}

	// nothing but white space comes after it
	if _, err := dec.Token(); err != io.EOF {
		return nil, "Invalid"
	}

	return v, ""
}

func readJSON(m rtime, dec *json.Decoder, depth int) (Value, string) {
	if depth > maxJSONDepth {
		return nil, "Invalid"
	}

	tok, err := dec.Token()
	if err != nil {
		return nil, "Invalid"
	}

	switch t := tok.(type) {
	case nil:
		return dataNull(m), ""
	case bool:
		return dataOf(m, "Bool", t), ""
	case string:
		return dataOf(m, "Text", t), ""
	case json.Number:
		if strings.ContainsAny(string(t), ".eE") {
			f, err := strconv.ParseFloat(string(t), 64)
			if err != nil || math.IsInf(f, 0) {
				return nil, "OutOfRange"
			}

			return dataOf(m, "Float", f), ""
		}

		n, err := strconv.ParseInt(string(t), 10, 64)
		if err != nil {
			return nil, "OutOfRange"
		}

		return dataOf(m, "Int", n), ""
	case json.Delim:
		if t == '[' {
			var items []Value

			for dec.More() {
				v, kind := readJSON(m, dec, depth+1)
				if kind != "" {
					return nil, kind
				}

				items = append(items, v)
			}

			if _, err := dec.Token(); err != nil {
				return nil, "Invalid"
			}

			return dataOf(m, "List", &List{&listBase{items}, len(items)}), ""
		}

		if t == '{' {
			out := NewMap()

			for dec.More() {
				key, err := dec.Token()

				k, isText := key.(string)
				if err != nil || !isText {
					return nil, "Invalid"
				}

				v, kind := readJSON(m, dec, depth+1)
				if kind != "" {
					return nil, kind
				}

				out = out.Set(k, v)
			}

			if _, err := dec.Token(); err != nil {
				return nil, "Invalid"
			}

			return dataOf(m, "Map", out), ""
		}
	}

	return nil, "Invalid"
}

// DataFromGo makes a Data of what Go has for JSON: nil, bool, the integers and floats, string, []any and map[string]any. The
// entries of a map[string]any are in the order of their keys, since Go has no other.
func (p *Program) DataFromGo(x any) (Value, error) {
	d := p.defs["Data"]
	mk := func(name string, fields ...Value) Value { return &Enum{d, variantOf(d, name), fields} }

	switch x := x.(type) {
	case nil:
		return mk("Null"), nil
	case bool:
		return mk("Bool", x), nil
	case int:
		return mk("Int", int64(x)), nil
	case int64:
		return mk("Int", x), nil
	case int32:
		return mk("Int", int64(x)), nil
	case float64:
		return mk("Float", x), nil
	case string:
		return mk("Text", x), nil
	case []any:
		items := make([]Value, len(x))

		for i, e := range x {
			v, err := p.DataFromGo(e)
			if err != nil {
				return nil, err
			}

			items[i] = v
		}

		return mk("List", &List{&listBase{items}, len(items)}), nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}

		out := NewMap()

		for _, k := range sorted(keys) {
			v, err := p.DataFromGo(x[k])
			if err != nil {
				return nil, err
			}

			out = out.Set(k, v)
		}

		return mk("Map", out), nil
	}

	return nil, fmt.Errorf("a %T is not data", x)
}

// DataToGo is the Go value of a Data: nil, bool, int64, float64, string, []any and map[string]any.
func DataToGo(v Value) (any, error) {
	e, ok := v.(*Enum)
	if !ok || e.Def.Name != "Data" {
		return nil, fmt.Errorf("%s is not a Data", Format(v))
	}

	switch e.Variant.Name {
	case "Null":
		return nil, nil
	case "List":
		items := e.Fields[0].(*List).Items()
		out := make([]any, len(items))

		for i, x := range items {
			g, err := DataToGo(x)
			if err != nil {
				return nil, err
			}

			out[i] = g
		}

		return out, nil
	case "Map":
		mp := e.Fields[0].(*Map)
		out := make(map[string]any, mp.Len())

		for i := range mp.Len() {
			k, x := mp.Entry(i)

			g, err := DataToGo(x)
			if err != nil {
				return nil, err
			}

			out[k.(string)] = g
		}

		return out, nil
	}

	return e.Fields[0], nil
}

// progRT is a program seen as what the library needs, for what is done outside a run: reading JSON, for one.
type progRT struct{ p *Program }

func (r progRT) program() *Program        { return r.p }
func (r progRT) context() context.Context { return context.Background() }
func (r progRT) callback(Value, ast.Node, ...Value) (Value, error) {
	return nil, fmt.Errorf("no program is running")
}

func (r progRT) fault(at ast.Node, kind, msg string) *Fault { return &Fault{Kind: kind, Msg: msg} }

// DataFromJSON reads JSON text as a Data, in the way Data::from_json does.
func (p *Program) DataFromJSON(text string) (Value, error) {
	v, kind := parseJSON(progRT{p}, text)
	if kind != "" {
		return nil, fmt.Errorf("not JSON that a Data can hold (%s)", kind)
	}

	return v, nil
}

// DataJSON writes a Data as JSON, in the way to_json does.
func DataJSON(v Value) (string, error) {
	e, ok := v.(*Enum)
	if !ok || e.Def.Name != "Data" {
		return "", fmt.Errorf("%s is not a Data", Format(v))
	}

	var sb strings.Builder
	if err := writeJSON(&sb, e); err != nil {
		return "", err
	}

	return sb.String(), nil
}
