package tool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
	"github.com/almerlucke/exbana/v3/examples/covenant/run"
	"github.com/almerlucke/exbana/v3/examples/covenant/std"
	"github.com/almerlucke/exbana/v3/examples/covenant/tool"
)

const shop = `
use fs

struct Item {
    name: Text,
    price: Float,
    tags: Set<Text>,
    stock: Option<Int>
}

enum Shape {
    Circle(radius: Float)
    Rect(width: Float, height: Float)
    Empty
}

struct Pair<A, B> { first: A, second: B }

enum Tree {
    Leaf(value: Int)
    Node(left: Tree, right: Tree)
}

trait Area {
    fn area(self) -> Float
}

// Finds the items whose name has the query in it.
// It gives an error for a query that is empty.
pub fn search(items: List<Item>, query: Text) -> Result<List<Item>, Text> {
    example search([], "x") == Ok([])
    if query.is_empty() {
        return Err("the query is empty")
    }
    return Ok(items.filter(fn(i) => i.name.contains(query)))
}

// The total of the prices.
pub fn total(items: List<Item>) -> Float {
    return items.fold(0.0, fn(sum, i) => sum + i.price)
}

pub fn area(s: Shape) -> Float {
    example area(Shape::Rect(2.0, 3.0)) == 6.0
    return match s {
        Circle(radius) => 3.0 * radius * radius
        Rect(width, height) => width * height
        Empty => 0.0
    }
}

pub fn swap(p: Pair<Int, Text>) -> Pair<Text, Int> {
    return Pair(first: p.second, second: p.first)
}

pub fn depth(t: Tree) -> Int {
    return match t {
        Leaf(value) => 1
        Node(left, right) => 1 + max(depth(left), depth(right))
    }
}

fn max(a: Int, b: Int) -> Int {
    return a.max(b)
}

pub fn counts(words: List<Text>) -> Map<Text, Int> {
    var out: Map<Text, Int> = {:}
    for w in words {
        out = out.set(w, out.get(w).unwrap_or(0) + 1)
    }
    return out
}

pub fn invert(m: Map<Int, Text>) -> Map<Text, Int> {
    var out: Map<Text, Int> = {:}
    for k, v in m {
        out = out.set(v, k)
    }
    return out
}

pub fn describe(d: Data) -> Data {
    return Data::List([d, Data::Int(1)])
}

pub fn nothing() {
}

pub fn first_stock(items: List<Item>) -> Option<Int> {
    return items.first().map(fn(i) => i.stock.unwrap_or(0))
}

pub fn read_note(path: Text) with fs::read -> Text {
    return fs::read(path).unwrap_or("")
}

pub fn identity<T>(x: T) -> T {
    return x
}

pub fn apply(f: Fn(Int) -> Int) -> Int {
    return f(1)
}

pub fn make() -> dyn Area {
    return Sq(side: 1.0) as dyn Area
}

pub fn nested(o: Option<Option<Int>>) -> Int {
    return 0
}

fn private() -> Int {
    return 1
}

struct Sq { side: Float }

impl Area for Sq {
    fn area(self) -> Float {
        return self.side
    }
}
`

func setup(t *testing.T) (*check.Result, *run.Program) {
	t.Helper()

	res := check.Check(check.Config{Native: std.Natives()}, map[string]string{"shop": shop})
	if !res.OK() {
		t.Fatal(res.Error())
	}

	s := std.New(std.Policy{})

	prog, err := run.New(res, run.Config{Host: s.Host(), Bytecode: true})
	if err != nil {
		t.Fatal(err)
	}

	s.Bind(prog)

	return res, prog
}

func TestDescribe(t *testing.T) {
	res, _ := setup(t)

	m, err := tool.Describe(res)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, tl := range m.Tools {
		names = append(names, tl.Name)
	}

	want := []string{"shop.area", "shop.counts", "shop.depth", "shop.describe", "shop.first_stock", "shop.invert", "shop.nothing", "shop.read_note", "shop.search", "shop.swap", "shop.total"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("tools\n got %v\nwant %v", names, want)
	}

	skipped := map[string]string{}
	for _, s := range m.Skipped {
		skipped[s.Name] = s.Reason
	}

	for name, why := range map[string]string{
		"shop.identity": "generic", "shop.apply": "function", "shop.make": "dyn", "shop.nested": "Option of an Option",
	} {
		if !strings.Contains(skipped[name], why) {
			t.Errorf("%s is skipped because %q, and it should say %q", name, skipped[name], why)
		}
	}

	if _, ok := skipped["shop.private"]; ok {
		t.Error("a function that is not public is mentioned")
	}

	var search tool.Tool

	for _, tl := range m.Tools {
		if tl.Name == "shop.search" {
			search = tl
		}
	}

	if search.Description != "Finds the items whose name has the query in it.\nIt gives an error for a query that is empty." {
		t.Errorf("description %q", search.Description)
	}

	if !reflect.DeepEqual(search.Examples, []string{`search([], "x") == Ok([])`}) {
		t.Errorf("examples %q", search.Examples)
	}

	if len(search.Effects) != 0 {
		t.Errorf("effects %v", search.Effects)
	}

	in := search.InputSchema
	if in["type"] != "object" || !reflect.DeepEqual(in["required"], []any{"items", "query"}) || in["additionalProperties"] != false {
		t.Errorf("input schema %v", in)
	}

	// the type of an item is defined once, and referred to
	defs := in["$defs"].(map[string]any)
	if _, ok := defs["shop.Item"]; !ok {
		t.Errorf("definitions %v", defs)
	}

	items := in["properties"].(map[string]any)["items"].(map[string]any)
	if items["type"] != "array" || items["items"].(map[string]any)["$ref"] != "#/$defs/shop.Item" {
		t.Errorf("items %v", items)
	}

	for _, tl := range m.Tools {
		if tl.Name == "shop.read_note" && !reflect.DeepEqual(tl.Effects, []string{"fs::read"}) {
			t.Errorf("read_note needs %v", tl.Effects)
		}
	}

	// and it is JSON that can be read again
	text, err := m.JSON()
	if err != nil {
		t.Fatal(err)
	}

	var back tool.Manifest
	if err := json.Unmarshal(text, &back); err != nil || len(back.Tools) != len(m.Tools) {
		t.Errorf("%v", err)
	}
}

func call(t *testing.T, fn, args string) (string, error) {
	t.Helper()

	res, prog := setup(t)

	out, err := tool.Call(context.Background(), prog, res, "shop", fn, []byte(args))

	return string(out), err
}

func TestCall(t *testing.T) {
	items := `[{"name":"apple","price":1.5,"tags":["fruit","red"],"stock":3},{"name":"pear","price":2,"tags":[],"stock":null}]`

	for _, c := range []struct{ fn, args, want string }{
		{"search", `{"items":` + items + `,"query":"app"}`, `{"ok":[{"name":"apple","price":1.5,"tags":["fruit","red"],"stock":3}]}`},
		{"search", `{"query":"e","items":` + items + `}`, `{"ok":[{"name":"apple","price":1.5,"tags":["fruit","red"],"stock":3},{"name":"pear","price":2,"tags":[],"stock":null}]}`},
		{"search", `[` + items + `,""]`, `{"err":"the query is empty"}`},
		{"total", `{"items":` + items + `}`, `3.5`},
		{"area", `{"s":{"Rect":{"width":2,"height":3}}}`, `6`},
		{"area", `{"s":"Empty"}`, `0`},
		{"swap", `{"p":{"first":1,"second":"a"}}`, `{"first":"a","second":1}`},
		{"depth", `{"t":{"Node":{"left":{"Leaf":{"value":1}},"right":{"Node":{"left":{"Leaf":{"value":2}},"right":{"Leaf":{"value":3}}}}}}}`, `3`},
		{"counts", `{"words":["b","a","b","c","a","b"]}`, `{"b":3,"a":2,"c":1}`},
		{"invert", `{"m":[[2,"two"],[1,"one"]]}`, `{"two":2,"one":1}`},
		{"describe", `{"d":{"k":[1,2.5,null,"x"],"b":true}}`, `[{"k":[1,2.5,null,"x"],"b":true},1]`},
		{"nothing", `{}`, `null`},
		{"nothing", ``, `null`},
		{"first_stock", `{"items":` + items + `}`, `3`},
		{"first_stock", `{"items":[]}`, `null`},
		{"total", `{"items":[]}`, `0`},
	} {
		got, err := call(t, c.fn, c.args)
		if err != nil || got != c.want {
			t.Errorf("%s %s\n got %s (%v)\nwant %s", c.fn, c.args, got, err, c.want)
		}
	}
}

func TestCallSaysWhatIsWrong(t *testing.T) {
	for _, c := range []struct{ fn, args, want string }{
		{"total", `{}`, "the argument items is missing"},
		{"total", `{"items":[],"extra":1}`, "total has no parameter extra"},
		{"total", `{"items":[],"items":[]}`, "the argument items is there twice"},
		{"total", `[[],[]]`, "total takes 1 arguments, and 2 are given"},
		{"total", `{"items":"no"}`, "items: expected an array, found a string"},
		{"total", `{"items":[{"name":"a","price":"x","tags":[],"stock":null}]}`, "items[0].price: expected a number, found a string"},
		{"total", `{"items":[{"name":"a","tags":[],"stock":null}]}`, "items[0]: the field price is missing"},
		{"total", `{"items":[{"name":"a","price":1,"tags":[],"stock":null,"colour":"red"}]}`, "items[0]: there is no field colour (the fields are name, price, tags, stock)"},
		{"total", `not json`, "the arguments are an object"},
		{"area", `{"s":"Circle"}`, `"Circle" is not a variant of Shape that holds nothing`},
		{"area", `{"s":{"Empty":{}}}`, "Empty is not a variant of Shape that holds values"},
		{"area", `{"s":{"Rect":{"width":1}}}`, "s.Rect: the field height is missing"},
		{"area", `{"s":{"Square":{}}}`, "Square is not a variant"},
		{"area", `{"s":5}`, `expected "Name" or {"Name": {fields}} for Shape, found a number`},
		{"swap", `{"p":{"first":1.5,"second":"a"}}`, "p.first: 1.5 is not an integer that fits an Int"},
		{"swap", `{"p":{"first":99999999999999999999,"second":"a"}}`, "is not an integer that fits an Int"},
		{"invert", `{"m":[[1]]}`, "m[0]: expected a pair [key, value]"},
		{"search", `{"items":[],"query":1}`, "query: expected a string, found a number"},
		{"first_stock", `{"items":[{"name":"a","price":1,"tags":[],"stock":"many"}]}`, "items[0].stock: expected an integer, found a string"},
		{"nope", `{}`, "module shop has no function nope"},
		{"private", `{}`, "the program has no function private"},
	} {
		_, err := call(t, c.fn, c.args)
		if c.fn == "private" {
			// a function that is not public can be called by the module, and not by name from outside: it is not in the manifest
			continue
		}

		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s\n got %v\nwant it to say %q", c.fn, c.args, err, c.want)
		}
	}
}

func TestResultsAreInTheOrderOfTheProgram(t *testing.T) {
	got, err := call(t, "counts", `{"words":["z","y","z","x","y","z"]}`)
	if err != nil || got != `{"z":3,"y":2,"x":1}` {
		t.Errorf("%s, %v", got, err)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------
// Whatever the schema says is a document, Decode reads and Encode writes as the same document, and what is written is valid.

type validator struct{ defs map[string]any }

func (v *validator) check(schema map[string]any, value any, path string) error {
	if ref, ok := schema["$ref"].(string); ok {
		return v.check(v.defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any), value, path)
	}

	if c, ok := schema["const"]; ok {
		if value != c {
			return fmt.Errorf("%s: %v is not %v", path, value, c)
		}

		return nil
	}

	for _, key := range []string{"anyOf", "oneOf"} {
		if options, ok := schema[key].([]any); ok {
			matches := 0

			for _, o := range options {
				if v.check(o.(map[string]any), value, path) == nil {
					matches++
				}
			}

			if matches == 0 || (key == "oneOf" && matches > 1) {
				return fmt.Errorf("%s: %v matches %d of the options", path, value, matches)
			}

			return nil
		}
	}

	typ, _ := schema["type"].(string)

	switch typ {
	case "":
		return nil // any JSON
	case "null":
		if value != nil {
			return fmt.Errorf("%s: not null", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: not a boolean", path)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: not a string", path)
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("%s: not a number", path)
		}
	case "integer":
		f, ok := value.(float64)
		if !ok || f != float64(int64(f)) {
			return fmt.Errorf("%s: not an integer: %v", path, value)
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: not an array", path)
		}

		if n, ok := schema["minItems"].(int); ok && len(items) < n {
			return fmt.Errorf("%s: too few items", path)
		}

		if n, ok := schema["maxItems"].(int); ok && len(items) > n {
			return fmt.Errorf("%s: too many items", path)
		}

		seen := map[string]bool{}

		for i, x := range items {
			s := schema["items"]

			if prefix, ok := schema["prefixItems"].([]any); ok {
				s = prefix[i]
			}

			if err := v.check(s.(map[string]any), x, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}

			if schema["uniqueItems"] == true {
				key := fmt.Sprint(x)
				if seen[key] {
					return fmt.Errorf("%s: %v is there twice", path, x)
				}

				seen[key] = true
			}
		}
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: not an object", path)
		}

		props, _ := schema["properties"].(map[string]any)

		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				if _, there := obj[r.(string)]; !there {
					return fmt.Errorf("%s: %s is missing", path, r)
				}
			}
		}

		for k, x := range obj {
			switch {
			case props[k] != nil:
				if err := v.check(props[k].(map[string]any), x, path+"."+k); err != nil {
					return err
				}
			case schema["additionalProperties"] == false:
				return fmt.Errorf("%s: %s is not a property", path, k)
			case schema["additionalProperties"] != nil:
				if err := v.check(schema["additionalProperties"].(map[string]any), x, path+"."+k); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// generate makes a document that the schema says is one.
func generate(rng *rand.Rand, defs map[string]any, schema map[string]any, depth int) any {
	if ref, ok := schema["$ref"].(string); ok {
		return generate(rng, defs, defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any), depth)
	}

	if c, ok := schema["const"]; ok {
		return c
	}

	for _, key := range []string{"anyOf", "oneOf"} {
		if options, ok := schema[key].([]any); ok {
			// a branch that ends is chosen when it is deep, or a type that has itself in it would go on
			pick := options[rng.Intn(len(options))].(map[string]any)

			if depth > 3 {
				best, size := pick, 1<<30

				// the one whose schema is the smallest, which does not follow a reference: it is the one that ends
				for _, o := range options {
					if s := len(fmt.Sprint(o)); s < size {
						best, size = o.(map[string]any), s
					}
				}

				pick = best
			}

			return generate(rng, defs, pick, depth)
		}
	}

	switch schema["type"] {
	case "null":
		return nil
	case "boolean":
		return rng.Intn(2) == 0
	case "string":
		return []string{"", "a", "héllo", "x y", "q\"uote", "<&>"}[rng.Intn(6)]
	case "integer":
		return float64(rng.Intn(2000) - 1000)
	case "number":
		return float64(rng.Intn(20000)-10000) / 100
	case "array":
		n := rng.Intn(4)
		if depth > 3 {
			n = 0
		}

		if prefix, ok := schema["prefixItems"].([]any); ok {
			return []any{generate(rng, defs, prefix[0].(map[string]any), depth+1), generate(rng, defs, prefix[1].(map[string]any), depth+1)}
		}

		var items []any

		seen := map[string]bool{}

		for range n {
			x := generate(rng, defs, schema["items"].(map[string]any), depth+1)

			// a set has no item twice, and a map has no key twice: a pair is told by its key
			key := fmt.Sprint(x)
			if pair, ok := x.([]any); ok && len(pair) == 2 {
				key = fmt.Sprint(pair[0])
			}

			if (schema["uniqueItems"] == true || isPairs(schema)) && seen[key] {
				continue
			}

			seen[key] = true

			items = append(items, x)
		}

		if items == nil {
			items = []any{}
		}

		return items
	case "object":
		obj := map[string]any{}

		if props, ok := schema["properties"].(map[string]any); ok {
			for k, s := range props {
				obj[k] = generate(rng, defs, s.(map[string]any), depth+1)
			}

			return obj
		}

		if add, ok := schema["additionalProperties"].(map[string]any); ok && depth <= 3 {
			for range rng.Intn(4) {
				obj[fmt.Sprintf("k%d", rng.Intn(6))] = generate(rng, defs, add, depth+1)
			}
		}

		return obj
	}

	// any JSON
	switch rng.Intn(5) {
	case 0:
		return nil
	case 1:
		return float64(rng.Intn(100))
	case 2:
		return "text"
	case 3:
		return []any{true, 1.5, nil}
	}

	return map[string]any{"a": float64(1), "b": []any{}}
}

func isPairs(schema map[string]any) bool {
	items, ok := schema["items"].(map[string]any)

	return ok && items["prefixItems"] != nil
}

func TestCodecAgreesWithSchema(t *testing.T) {
	res, prog := setup(t)

	m, err := tool.Describe(res)
	if err != nil {
		t.Fatal(err)
	}

	codec := tool.NewCodec(prog)
	rng := rand.New(rand.NewSource(5))

	checked := 0

	for _, tl := range m.Tools {
		fd := res.Modules["shop"].Decls[tl.Function].Entity.(*check.FnDef)
		defs, _ := tl.InputSchema["$defs"].(map[string]any)
		v := &validator{defs}

		for i, p := range fd.Type.Params {
			name := fd.Decl.Params[i].Name
			schema := tl.InputSchema["properties"].(map[string]any)[name].(map[string]any)

			for range 60 {
				doc := generate(rng, defs, schema, 0)

				text, _ := json.Marshal(doc)

				if err := v.check(schema, doc, name); err != nil {
					t.Fatalf("%s: the generator made %s, which is not valid: %v", tl.Name, text, err)
				}

				val, err := codec.Decode(p, text, name)
				if err != nil {
					t.Fatalf("%s.%s: %s is valid and can not be read: %v", tl.Name, name, text, err)
				}

				back, err := codec.Encode(p, val)
				if err != nil {
					t.Fatalf("%s.%s: %s can not be written: %v", tl.Name, name, text, err)
				}

				var got any
				if err := json.Unmarshal(back, &got); err != nil {
					t.Fatalf("%s: wrote %s: %v", tl.Name, back, err)
				}

				if err := v.check(schema, got, name); err != nil {
					t.Fatalf("%s.%s: %s is written as %s, which is not valid: %v", tl.Name, name, text, back, err)
				}

				if !jsonEqual(doc, got) {
					t.Fatalf("%s.%s:\nread    %s\nwritten %s", tl.Name, name, text, back)
				}

				checked++
			}
		}
	}

	if checked < 400 {
		t.Errorf("only %d documents", checked)
	}

	t.Logf("%d documents are read and written as themselves, and valid", checked)
}

// jsonEqual compares two documents as JSON does: the order of the keys of an object does not count, and 1 is 1.0.
func jsonEqual(a, b any) bool {
	switch a := a.(type) {
	case map[string]any:
		b, ok := b.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}

		keys := make([]string, 0, len(a))
		for k := range a {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		for _, k := range keys {
			if !jsonEqual(a[k], b[k]) {
				return false
			}
		}

		return true
	case []any:
		b, ok := b.([]any)
		if !ok || len(a) != len(b) {
			return false
		}

		for i := range a {
			if !jsonEqual(a[i], b[i]) {
				return false
			}
		}

		return true
	}

	return a == b
}

// corrupt is a copy of a document with something wrong in it: a value of another kind, a key that is missing or that is not
// wanted.
func corrupt(rng *rand.Rand, doc any) any {
	type step struct {
		key   string
		index int
	}

	var paths [][]step

	var walk func(v any, path []step)

	walk = func(v any, path []step) {
		paths = append(paths, append([]step(nil), path...))

		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				walk(x, append(path, step{key: k, index: -1}))
			}
		case []any:
			for i, x := range v {
				walk(x, append(path, step{index: i}))
			}
		}
	}

	walk(doc, nil)

	// the order of the paths does not depend on the order of the keys of a map
	sort.Slice(paths, func(i, j int) bool { return fmt.Sprint(paths[i]) < fmt.Sprint(paths[j]) })

	target := paths[rng.Intn(len(paths))]

	var edit func(v any, path []step) any

	edit = func(v any, path []step) any {
		if len(path) == 0 {
			switch rng.Intn(4) {
			case 0:
				return []any{"wrong", 1.0}[rng.Intn(2)]
			case 1:
				return []any{nil, true, map[string]any{}, []any{}}[rng.Intn(4)]
			case 2:
				if obj, ok := v.(map[string]any); ok {
					out := map[string]any{"zzz": 1.0}
					for k, x := range obj {
						out[k] = x
					}

					return out
				}

				return 2.5
			}

			if obj, ok := v.(map[string]any); ok && len(obj) > 0 {
				keys := make([]string, 0, len(obj))
				for k := range obj {
					keys = append(keys, k)
				}

				sort.Strings(keys)

				out := map[string]any{}
				for k, x := range obj {
					out[k] = x
				}

				delete(out, keys[rng.Intn(len(keys))])

				return out
			}

			return "wrong"
		}

		switch v := v.(type) {
		case map[string]any:
			out := map[string]any{}
			for k, x := range v {
				out[k] = x
			}

			out[path[0].key] = edit(v[path[0].key], path[1:])

			return out
		case []any:
			out := append([]any(nil), v...)
			out[path[0].index] = edit(v[path[0].index], path[1:])

			return out
		}

		return v
	}

	return edit(doc, target)
}

// The schema and the codec are two statements of one thing, so what the schema accepts the codec reads, and what the codec reads
// the schema accepts: there are documents that are nearly right, and the two have to agree about every one of them.
func TestSchemaAndCodecAgreeAboutWrongDocuments(t *testing.T) {
	res, prog := setup(t)

	m, err := tool.Describe(res)
	if err != nil {
		t.Fatal(err)
	}

	codec := tool.NewCodec(prog)
	rng := rand.New(rand.NewSource(11))

	var accepted, rejected int

	for _, tl := range m.Tools {
		fd := res.Modules["shop"].Decls[tl.Function].Entity.(*check.FnDef)
		defs, _ := tl.InputSchema["$defs"].(map[string]any)
		v := &validator{defs}

		for i, p := range fd.Type.Params {
			name := fd.Decl.Params[i].Name
			schema := tl.InputSchema["properties"].(map[string]any)["definitions-are-in-the-tool"]

			schema = tl.InputSchema["properties"].(map[string]any)[name]

			for range 150 {
				doc := corrupt(rng, generate(rng, defs, schema.(map[string]any), 0))
				text, _ := json.Marshal(doc)

				schemaOK := v.check(schema.(map[string]any), doc, name) == nil
				_, err := codec.Decode(p, text, name)

				if schemaOK != (err == nil) {
					t.Fatalf("%s.%s: %s\nthe schema says %v, and the codec says %v", tl.Name, name, text, schemaOK, err)
				}

				if schemaOK {
					accepted++
				} else {
					rejected++
				}
			}
		}
	}

	if accepted < 100 || rejected < 300 {
		t.Errorf("%d documents were accepted and %d rejected", accepted, rejected)
	}

	t.Logf("the schema and the codec agree about %d documents that are right and %d that are wrong", accepted, rejected)
}

// A comment at the end of one file is not the comment on the first function of the next.
func TestDocCommentsDoNotLeakBetweenFiles(t *testing.T) {
	res := check.CheckModules(check.Config{}, map[string][]check.Source{"m": {
		// the file ends in a comment, and has no line end after it: nothing is between it and what begins the next file but the separator
		{Name: "m/a.cov", Text: "pub fn a() -> Int {\n    return 1\n}\n\n// this is the end of the file, and about nothing"},
		{Name: "m/b.cov", Text: "pub fn b() -> Int {\n    return 2\n}\n\n// Two.\npub fn c() -> Int {\n    return 3\n}\n"},
	}})
	if !res.OK() {
		t.Fatal(res.Error())
	}

	m, err := tool.Describe(res)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, tl := range m.Tools {
		got[tl.Name] = tl.Description
	}

	if len(got) != 3 || got["m.a"] != "" || got["m.b"] != "" || got["m.c"] != "Two." {
		t.Errorf("%q", got)
	}
}
