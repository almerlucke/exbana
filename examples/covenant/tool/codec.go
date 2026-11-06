package tool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
	"github.com/almerlucke/exbana/v3/examples/covenant/run"
)

// Codec turns JSON into the values of a program, and the values of a program into JSON, for the types of the functions of it.
type Codec struct {
	prog *run.Program
}

// NewCodec makes the codec of a program.
func NewCodec(prog *run.Program) *Codec { return &Codec{prog} }

type kv struct {
	key string
	raw json.RawMessage
}

// object is the entries of a JSON object, in the order they are written.
func object(raw json.RawMessage) ([]kv, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))

	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("expected an object")
	}

	var out []kv

	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}

		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}

		out = append(out, kv{key.(string), v})
	}

	return out, nil
}

func array(raw json.RawMessage) ([]json.RawMessage, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || bytes.TrimSpace(raw)[0] != '[' {
		return nil, errors.New("expected an array")
	}

	return items, nil
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

func kind(raw json.RawMessage) string {
	switch b := bytes.TrimSpace(raw); {
	case len(b) == 0:
		return "nothing"
	case b[0] == '{':
		return "an object"
	case b[0] == '[':
		return "an array"
	case b[0] == '"':
		return "a string"
	case b[0] == 't' || b[0] == 'f':
		return "a boolean"
	case b[0] == 'n':
		return "null"
	}

	return "a number"
}

func at(path, msg string, args ...any) error {
	return fmt.Errorf("%s: %s", path, fmt.Sprintf(msg, args...))
}

// Decode makes the value of a type from JSON. path is where the JSON is, for the message about what is wrong with it.
func (c *Codec) Decode(t check.Type, raw json.RawMessage, path string) (run.Value, error) {
	sh, con := shapeOf(t)

	wrong := func(want string) error { return at(path, "expected %s, found %s", want, kind(raw)) }

	switch sh {
	case shInt:
		var n json.Number

		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()

		if err := dec.Decode(&n); err != nil || kind(raw) != "a number" {
			return nil, wrong("an integer")
		}

		if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
			return i, nil
		}

		// 2.0 is the integer 2, and 2.5 is not one
		if f, err := n.Float64(); err == nil && f == math.Trunc(f) && f >= -9.2e18 && f <= 9.2e18 {
			return int64(f), nil
		}

		return nil, at(path, "%s is not an integer that fits an Int", n)
	case shFloat:
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil || kind(raw) != "a number" {
			return nil, wrong("a number")
		}

		return f, nil
	case shText:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || kind(raw) != "a string" {
			return nil, wrong("a string")
		}

		return s, nil
	case shBool:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil || kind(raw) != "a boolean" {
			return nil, wrong("a boolean")
		}

		return b, nil
	case shUnit:
		if !isNull(raw) {
			return nil, wrong("null")
		}

		return run.Unit{}, nil
	case shData:
		return c.prog.DataFromJSON(string(raw))
	case shList, shSet:
		items, err := array(raw)
		if err != nil {
			return nil, wrong("an array")
		}

		vals := make([]run.Value, len(items))

		for i, x := range items {
			if vals[i], err = c.Decode(con.Args[0], x, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return nil, err
			}
		}

		if sh == shList {
			return run.NewList(vals), nil
		}

		set := run.NewSet()
		for _, v := range vals {
			set = set.Add(v)
		}

		return set, nil
	case shMap:
		out := run.NewMap()

		if k, _ := shapeOf(con.Args[0]); k == shText {
			entries, err := object(raw)
			if err != nil {
				return nil, wrong("an object")
			}

			for _, e := range entries {
				v, err := c.Decode(con.Args[1], e.raw, path+"."+e.key)
				if err != nil {
					return nil, err
				}

				out = out.Set(e.key, v)
			}

			return out, nil
		}

		pairs, err := array(raw)
		if err != nil {
			return nil, wrong("an array of pairs")
		}

		for i, p := range pairs {
			kvs, err := array(p)
			if err != nil || len(kvs) != 2 {
				return nil, at(fmt.Sprintf("%s[%d]", path, i), "expected a pair [key, value]")
			}

			key, err := c.Decode(con.Args[0], kvs[0], fmt.Sprintf("%s[%d][0]", path, i))
			if err != nil {
				return nil, err
			}

			val, err := c.Decode(con.Args[1], kvs[1], fmt.Sprintf("%s[%d][1]", path, i))
			if err != nil {
				return nil, err
			}

			out = out.Set(key, val)
		}

		return out, nil
	case shOption:
		if isNull(raw) {
			return c.prog.None(), nil
		}

		v, err := c.Decode(con.Args[0], raw, path)
		if err != nil {
			return nil, err
		}

		return c.prog.Some(v), nil
	case shResult:
		entries, err := object(raw)
		if err != nil || len(entries) != 1 || (entries[0].key != "ok" && entries[0].key != "err") {
			return nil, at(path, `expected {"ok": value} or {"err": error}`)
		}

		if entries[0].key == "ok" {
			v, err := c.Decode(con.Args[0], entries[0].raw, path+".ok")
			if err != nil {
				return nil, err
			}

			return c.prog.Ok(v), nil
		}

		v, err := c.Decode(con.Args[1], entries[0].raw, path+".err")
		if err != nil {
			return nil, err
		}

		return c.prog.Err(v), nil
	case shStruct:
		fields, err := c.fields(con, con.Def.Fields, raw, path)
		if err != nil {
			return nil, err
		}

		return &run.Struct{Def: con.Def, Fields: fields}, nil
	case shEnum:
		var name string

		if err := json.Unmarshal(raw, &name); err == nil && kind(raw) == "a string" {
			for _, v := range con.Def.Variants {
				if v.Name == name && len(v.Fields) == 0 {
					return &run.Enum{Def: con.Def, Variant: v}, nil
				}
			}

			return nil, at(path, "%q is not a variant of %s that holds nothing", name, con.Def.Name)
		}

		entries, err := object(raw)
		if err != nil || len(entries) != 1 {
			return nil, at(path, `expected "Name" or {"Name": {fields}} for %s, found %s`, con.Def.Name, kind(raw))
		}

		for _, v := range con.Def.Variants {
			if v.Name == entries[0].key && len(v.Fields) > 0 {
				fields, err := c.fields(con, v.Fields, entries[0].raw, path+"."+v.Name)
				if err != nil {
					return nil, err
				}

				return &run.Enum{Def: con.Def, Variant: v, Fields: fields}, nil
			}
		}

		return nil, at(path, "%s is not a variant of %s that holds values", entries[0].key, con.Def.Name)
	}

	return nil, at(path, "a %s can not be made of JSON", check.Resolve(t))
}

// fields makes the values of the fields of an object: every one of them is there, and no others.
func (c *Codec) fields(con *check.Con, infos []*check.FieldInfo, raw json.RawMessage, path string) ([]run.Value, error) {
	entries, err := object(raw)
	if err != nil {
		return nil, at(path, "expected an object, found %s", kind(raw))
	}

	vals := make([]run.Value, len(infos))
	seen := make([]bool, len(infos))

	for _, e := range entries {
		idx := -1

		for i, f := range infos {
			if f.Name == e.key {
				idx = i
			}
		}

		if idx < 0 {
			names := make([]string, len(infos))
			for i, f := range infos {
				names[i] = f.Name
			}

			return nil, at(path, "there is no field %s (the fields are %s)", e.key, strings.Join(names, ", "))
		}

		if seen[idx] {
			return nil, at(path, "the field %s is there twice", e.key)
		}

		seen[idx] = true

		if vals[idx], err = c.Decode(check.FieldType(con, infos[idx]), e.raw, path+"."+e.key); err != nil {
			return nil, err
		}
	}

	for i, f := range infos {
		if !seen[i] {
			return nil, at(path, "the field %s is missing", f.Name)
		}
	}

	return vals, nil
}

// Encode writes the value of a type as JSON.
func (c *Codec) Encode(t check.Type, v run.Value) ([]byte, error) {
	var sb strings.Builder

	if err := c.encode(&sb, t, v); err != nil {
		return nil, err
	}

	return []byte(sb.String()), nil
}

func quote(s string) string {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)

	return strings.TrimSuffix(buf.String(), "\n")
}

func (c *Codec) encode(sb *strings.Builder, t check.Type, v run.Value) error {
	sh, con := shapeOf(t)

	switch sh {
	case shInt:
		sb.WriteString(strconv.FormatInt(v.(int64), 10))
	case shFloat:
		f := v.(float64)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return errors.New("a Float that is not a number has no JSON")
		}

		out, _ := json.Marshal(f)
		sb.Write(out)
	case shText:
		sb.WriteString(quote(v.(string)))
	case shBool:
		sb.WriteString(strconv.FormatBool(v.(bool)))
	case shUnit:
		sb.WriteString("null")
	case shData:
		out, err := run.DataJSON(v)
		if err != nil {
			return err
		}

		sb.WriteString(out)
	case shList:
		sb.WriteByte('[')

		for i, x := range v.(*run.List).Items() {
			if i > 0 {
				sb.WriteByte(',')
			}

			if err := c.encode(sb, con.Args[0], x); err != nil {
				return err
			}
		}

		sb.WriteByte(']')
	case shSet:
		set := v.(*run.Set)

		sb.WriteByte('[')

		for i := range set.Len() {
			if i > 0 {
				sb.WriteByte(',')
			}

			if err := c.encode(sb, con.Args[0], set.Item(i)); err != nil {
				return err
			}
		}

		sb.WriteByte(']')
	case shMap:
		m := v.(*run.Map)
		textKeys := false

		if k, _ := shapeOf(con.Args[0]); k == shText {
			textKeys = true
		}

		sb.WriteByte(map[bool]byte{true: '{', false: '['}[textKeys])

		for i := range m.Len() {
			key, val := m.Entry(i)
			if i > 0 {
				sb.WriteByte(',')
			}

			if textKeys {
				sb.WriteString(quote(key.(string)) + ":")
			} else {
				sb.WriteByte('[')

				if err := c.encode(sb, con.Args[0], key); err != nil {
					return err
				}

				sb.WriteByte(',')
			}

			if err := c.encode(sb, con.Args[1], val); err != nil {
				return err
			}

			if !textKeys {
				sb.WriteByte(']')
			}
		}

		sb.WriteByte(map[bool]byte{true: '}', false: ']'}[textKeys])
	case shOption:
		e := v.(*run.Enum)
		if e.Variant.Name == "None" {
			sb.WriteString("null")
			return nil
		}

		return c.encode(sb, con.Args[0], e.Fields[0])
	case shResult:
		e := v.(*run.Enum)
		if e.Variant.Name == "Ok" {
			sb.WriteString(`{"ok":`)

			if err := c.encode(sb, con.Args[0], e.Fields[0]); err != nil {
				return err
			}
		} else {
			sb.WriteString(`{"err":`)

			if err := c.encode(sb, con.Args[1], e.Fields[0]); err != nil {
				return err
			}
		}

		sb.WriteByte('}')
	case shStruct:
		return c.writeFields(sb, con, con.Def.Fields, v.(*run.Struct).Fields)
	case shEnum:
		e := v.(*run.Enum)
		if len(e.Variant.Fields) == 0 {
			sb.WriteString(quote(e.Variant.Name))
			return nil
		}

		sb.WriteString("{" + quote(e.Variant.Name) + ":")

		if err := c.writeFields(sb, con, e.Variant.Fields, e.Fields); err != nil {
			return err
		}

		sb.WriteByte('}')
	default:
		return fmt.Errorf("a %s can not be made into JSON", check.Resolve(t))
	}

	return nil
}

func (c *Codec) writeFields(sb *strings.Builder, con *check.Con, infos []*check.FieldInfo, vals []run.Value) error {
	sb.WriteByte('{')

	for i, f := range infos {
		if i > 0 {
			sb.WriteByte(',')
		}

		sb.WriteString(quote(f.Name) + ":")

		if err := c.encode(sb, check.FieldType(con, f), vals[i]); err != nil {
			return err
		}
	}

	sb.WriteByte('}')

	return nil
}
