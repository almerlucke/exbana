// Package run is an interpreter for Covenant programs that the checker (see the check package) has found to be right. It runs
// what the checker has proven, and holds the program to the rest of what the language promises: overflow and division by zero
// are faults, a requires that is false is a fault, and a program only gets the effects the host grants.
//
// A value is a Go value of one of these types: int64 (Int), float64 (Float), string (Text), bool (Bool), Unit, *List, *Map, *Set,
// *Struct, *Enum (Option and Result are enums) and *Func. Values never change; the operations on a List, a Map and a Set make a
// new one, and share what they can with the old.
package run

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
)

// Value is a value of a program.
type Value = any

// Unit is the value of Unit.
type Unit struct{}

// List is a list. A list that was made by adding to another shares its elements: adding to the latest version of a list costs
// the same as appending to a slice, and adding to an older one copies.
type List struct {
	base *listBase
	n    int
}

type listBase struct{ vals []Value }

// NewList makes a list of the items.
func NewList(items []Value) *List {
	return &List{&listBase{append([]Value(nil), items...)}, len(items)}
}

// Len is the number of items.
func (l *List) Len() int { return l.n }

// At is the item at i.
func (l *List) At(i int) Value { return l.base.vals[i] }

// Items are the items. The slice is not to be changed.
func (l *List) Items() []Value { return l.base.vals[:l.n:l.n] }

// Push is the list with the item added to the end.
func (l *List) Push(v Value) *List {
	if l.n == len(l.base.vals) {
		l.base.vals = append(l.base.vals, v)
		return &List{l.base, l.n + 1}
	}

	return NewList(append(append([]Value(nil), l.Items()...), v))
}

// Map is a map that keeps its keys in the order they were added. A key that is new is added without copying, as for a List.
type Map struct {
	base *mapBase
	n    int
}

type mapBase struct {
	keys, vals []Value
	idx        map[string]int
}

// NewMap makes an empty map.
func NewMap() *Map { return &Map{&mapBase{idx: map[string]int{}}, 0} }

// Len is the number of entries.
func (m *Map) Len() int { return m.n }

// Entry is the key and the value at a place in the order.
func (m *Map) Entry(i int) (key, value Value) { return m.base.keys[i], m.base.vals[i] }

// Get is the value of a key.
func (m *Map) Get(key Value) (Value, bool) {
	if i, ok := m.base.idx[keyOf(key)]; ok && i < m.n {
		return m.base.vals[i], true
	}

	return nil, false
}

// Set is the map with the key set to the value. A key that is there keeps its place.
func (m *Map) Set(key, value Value) *Map {
	k := keyOf(key)

	i, there := m.base.idx[k]
	if there && i < m.n {
		out := m.copy()
		out.base.vals[i] = value

		return out
	}

	if m.n == len(m.base.keys) && !there {
		m.base.keys = append(m.base.keys, key)
		m.base.vals = append(m.base.vals, value)
		m.base.idx[k] = m.n

		return &Map{m.base, m.n + 1}
	}

	out := m.copy()
	out.base.keys, out.base.vals = append(out.base.keys, key), append(out.base.vals, value)
	out.base.idx[k] = out.n
	out.n++

	return out
}

// Remove is the map without the key.
func (m *Map) Remove(key Value) *Map {
	k := keyOf(key)
	if i, ok := m.base.idx[k]; !ok || i >= m.n {
		return m
	}

	out := NewMap()

	for i := 0; i < m.n; i++ {
		if keyOf(m.base.keys[i]) != k {
			out = out.Set(m.base.keys[i], m.base.vals[i])
		}
	}

	return out
}

func (m *Map) copy() *Map {
	out := &Map{&mapBase{
		keys: append([]Value(nil), m.base.keys[:m.n]...),
		vals: append([]Value(nil), m.base.vals[:m.n]...),
		idx:  make(map[string]int, m.n),
	}, m.n}

	for i, k := range out.base.keys {
		out.base.idx[keyOf(k)] = i
	}

	return out
}

// Set is a set that keeps its items in the order they were added.
type Set struct{ m *Map }

// NewSet makes an empty set.
func NewSet() *Set { return &Set{NewMap()} }

// Len is the number of items.
func (s *Set) Len() int { return s.m.Len() }

// Item is the item at a place in the order.
func (s *Set) Item(i int) Value { k, _ := s.m.Entry(i); return k }

// Has says whether the item is in the set.
func (s *Set) Has(v Value) bool { _, ok := s.m.Get(v); return ok }

// Add is the set with the item added.
func (s *Set) Add(v Value) *Set {
	if s.Has(v) {
		return s
	}

	return &Set{s.m.Set(v, Unit{})}
}

// Remove is the set without the item.
func (s *Set) Remove(v Value) *Set { return &Set{s.m.Remove(v)} }

// Obj is a value that is known by a trait: what "as dyn Area" makes, with the trait it was made for. A method is found from that
// trait, so a type that has two traits with a method of the same name is called the right way.
type Obj struct {
	Trait *check.TraitDef
	V     Value
}

// Struct is a value of a struct. Host is for the host to keep what a type that it provides stands for.
type Struct struct {
	Def    *check.Def
	Fields []Value
	Host   any
}

// Enum is a value of an enum: which variant, and what it holds.
type Enum struct {
	Def     *check.Def
	Variant *check.Variant
	Fields  []Value
}

// Field is the value of a field of a struct, by name.
func (s *Struct) Field(name string) (Value, bool) {
	for i, f := range s.Def.Fields {
		if f.Name == name {
			return s.Fields[i], true
		}
	}

	return nil, false
}

// keyOf is a string that is the same for equal values and for no others: it is what a map and a set tell their keys apart by.
func keyOf(v Value) string {
	var sb strings.Builder
	writeKey(&sb, v)

	return sb.String()
}

func writeKey(sb *strings.Builder, v Value) {
	switch v := v.(type) {
	case int64:
		sb.WriteString("i" + strconv.FormatInt(v, 10) + ";")
	case float64:
		sb.WriteString("f" + strconv.FormatUint(math.Float64bits(v), 16) + ";")
	case string:
		sb.WriteString("s" + strconv.Itoa(len(v)) + ":" + v)
	case bool:
		sb.WriteString(map[bool]string{true: "T;", false: "F;"}[v])
	case Unit:
		sb.WriteString("u;")
	case *Obj:
		sb.WriteString("O" + v.Trait.Name + "(")
		writeKey(sb, v.V)
		sb.WriteString(")")
	case *Struct:
		sb.WriteString("S" + v.Def.Module + "." + v.Def.Name + "(")

		for _, f := range v.Fields {
			writeKey(sb, f)
		}

		sb.WriteString(")")
	case *Enum:
		sb.WriteString("E" + v.Def.Module + "." + v.Def.Name + "." + v.Variant.Name + "(")

		for _, f := range v.Fields {
			writeKey(sb, f)
		}

		sb.WriteString(")")
	case *List:
		sb.WriteString("L[")

		for _, x := range v.Items() {
			writeKey(sb, x)
		}

		sb.WriteString("]")
	case *Map:
		// the same entries are the same map, in whatever order they were added
		parts := make([]string, v.Len())

		for i := range parts {
			k, val := v.Entry(i)
			parts[i] = keyOf(k) + "=" + keyOf(val)
		}

		sort.Strings(parts)
		sb.WriteString("M{" + strings.Join(parts, ",") + "}")
	case *Set:
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = keyOf(v.Item(i))
		}

		sort.Strings(parts)
		sb.WriteString("Z{" + strings.Join(parts, ",") + "}")
	default:
		fmt.Fprintf(sb, "?%p", v)
	}
}

// Equal says whether two values are equal: the same, field by field. A Float is equal to the same Float, and a NaN to nothing.
func Equal(a, b Value) bool {
	switch a := a.(type) {
	case float64:
		b, ok := b.(float64)
		return ok && a == b
	case *List:
		b, ok := b.(*List)
		if !ok || a.Len() != b.Len() {
			return false
		}

		for i := range a.Len() {
			if !Equal(a.At(i), b.At(i)) {
				return false
			}
		}

		return true
	case *Obj:
		b, ok := b.(*Obj)
		return ok && a.Trait == b.Trait && Equal(a.V, b.V)
	case *Struct:
		b, ok := b.(*Struct)
		return ok && a.Def == b.Def && equalAll(a.Fields, b.Fields)
	case *Enum:
		b, ok := b.(*Enum)
		return ok && a.Def == b.Def && a.Variant == b.Variant && equalAll(a.Fields, b.Fields)
	case *Map:
		b, ok := b.(*Map)
		if !ok || a.Len() != b.Len() {
			return false
		}

		for i := range a.Len() {
			k, v := a.Entry(i)

			w, there := b.Get(k)
			if !there || !Equal(v, w) {
				return false
			}
		}

		return true
	case *Set:
		b, ok := b.(*Set)
		if !ok || a.Len() != b.Len() {
			return false
		}

		for i := range a.Len() {
			if !b.Has(a.Item(i)) {
				return false
			}
		}

		return true
	}

	return a == b
}

func equalAll(a, b []Value) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !Equal(a[i], b[i]) {
			return false
		}
	}

	return true
}

// Format writes a value the way a program would write it: Some(1), [1, 2], {"a": 1}, Point(x: 1.0, y: 2.0).
func Format(v Value) string {
	var sb strings.Builder
	format(&sb, v)

	return sb.String()
}

func format(sb *strings.Builder, v Value) {
	switch v := v.(type) {
	case int64:
		sb.WriteString(strconv.FormatInt(v, 10))
	case float64:
		sb.WriteString(formatFloat(v))
	case string:
		sb.WriteString(strconv.Quote(v))
	case bool:
		sb.WriteString(strconv.FormatBool(v))
	case Unit:
		sb.WriteString("Unit")
	case *List:
		sb.WriteString("[")

		for i, x := range v.Items() {
			if i > 0 {
				sb.WriteString(", ")
			}

			format(sb, x)
		}

		sb.WriteString("]")
	case *Map:
		if v.Len() == 0 {
			sb.WriteString("{:}")
			return
		}

		sb.WriteString("{")

		for i := range v.Len() {
			if i > 0 {
				sb.WriteString(", ")
			}

			k, val := v.Entry(i)
			format(sb, k)
			sb.WriteString(": ")
			format(sb, val)
		}

		sb.WriteString("}")
	case *Set:
		sb.WriteString("Set::from([")

		for i := range v.Len() {
			if i > 0 {
				sb.WriteString(", ")
			}

			format(sb, v.Item(i))
		}

		sb.WriteString("])")
	case *Struct:
		sb.WriteString(v.Def.Name)
		formatFields(sb, v.Def.Fields, v.Fields, true)
	case *Enum:
		// the enums of the prelude are written as a program writes them, with what they hold by position: Some(1), Data::Int(7)
		prelude := v.Def.Module == "prelude"
		if !prelude || (v.Def.Name != "Option" && v.Def.Name != "Result") {
			sb.WriteString(v.Def.Name + "::")
		}

		sb.WriteString(v.Variant.Name)
		formatFields(sb, v.Variant.Fields, v.Fields, !prelude)
	case *Obj:
		format(sb, v.V)
	case *Func:
		sb.WriteString("<function " + v.name + ">")
	default:
		fmt.Fprintf(sb, "%v", v)
	}
}

func formatFields(sb *strings.Builder, infos []*check.FieldInfo, vals []Value, named bool) {
	if len(infos) == 0 {
		return
	}

	sb.WriteString("(")

	for i, f := range infos {
		if i > 0 {
			sb.WriteString(", ")
		}

		if named {
			sb.WriteString(f.Name + ": ")
		}

		format(sb, vals[i])
	}

	sb.WriteString(")")
}

// formatFloat writes a Float so that it reads as a Float: 1.0 and not 1.
func formatFloat(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEIN") {
		s += ".0"
	}

	return s
}
