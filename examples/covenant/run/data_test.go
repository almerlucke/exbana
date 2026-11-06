package run_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/covenant/run"
)

// A random document, kept in the order of its keys, to be written as JSON in a way that is hard to read and as the JSON that
// Data writes.
type doc struct {
	kind  int // 0 null, 1 bool, 2 int, 3 float, 4 text, 5 list, 6 map
	b     bool
	i     int64
	f     float64
	s     string
	items []doc
	keys  []string
}

var texts = []string{"", "a", "é", "é\u2028", "\"", "\\", "line\nbreak\ttab", "<tag> & more", "\u0001", "日本語", "😀"}
var floats = []float64{0.5, -1.25, 1e21, 1.5e-7, 100.0, 3.141592653589793, -0.0001, 2.5e300}
var ints = []int64{0, 1, -1, 42, 1 << 40, -9223372036854775808, 9223372036854775807}

func randomDoc(rng *rand.Rand, depth int) doc {
	k := rng.Intn(7)
	if depth > 4 && k > 4 {
		k = rng.Intn(5)
	}

	d := doc{kind: k}

	switch k {
	case 1:
		d.b = rng.Intn(2) == 0
	case 2:
		d.i = ints[rng.Intn(len(ints))]
	case 3:
		d.f = floats[rng.Intn(len(floats))]
	case 4:
		d.s = texts[rng.Intn(len(texts))]
	case 5:
		for range rng.Intn(4) {
			d.items = append(d.items, randomDoc(rng, depth+1))
		}
	case 6:
		for i := range rng.Intn(4) {
			d.keys = append(d.keys, texts[rng.Intn(len(texts))]+strconv.Itoa(i))
			d.items = append(d.items, randomDoc(rng, depth+1))
		}
	}

	return d
}

// write the document as text: messy, with white space and every escape that is allowed, or as Data writes it.
func (d doc) write(sb *strings.Builder, rng *rand.Rand, messy bool) {
	space := func() {
		if messy {
			sb.WriteString([]string{"", " ", "\n", "\t ", "  "}[rng.Intn(5)])
		}
	}

	quote := func(s string) {
		if !messy {
			var buf bytes.Buffer

			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			_ = enc.Encode(s)
			sb.WriteString(strings.TrimSuffix(buf.String(), "\n"))

			return
		}

		out, _ := json.Marshal(s)
		sb.WriteString(string(out))
	}

	switch d.kind {
	case 0:
		sb.WriteString("null")
	case 1:
		sb.WriteString(strconv.FormatBool(d.b))
	case 2:
		sb.WriteString(strconv.FormatInt(d.i, 10))
	case 3:
		out, _ := json.Marshal(d.f)

		sb.Write(out)

		// a Float is written so that it reads as one, in the messy text as well
		if !bytes.ContainsAny(out, ".eE") {
			sb.WriteString(".0")
		}
	case 4:
		quote(d.s)
	case 5:
		sb.WriteString("[")
		space()

		for i, x := range d.items {
			if i > 0 {
				sb.WriteString(",")
				space()
			}

			x.write(sb, rng, messy)
			space()
		}

		sb.WriteString("]")
	case 6:
		sb.WriteString("{")
		space()

		for i, x := range d.items {
			if i > 0 {
				sb.WriteString(",")
				space()
			}

			quote(d.keys[i])
			space()
			sb.WriteString(":")
			space()
			x.write(sb, rng, messy)
			space()
		}

		sb.WriteString("}")
	}
}

func TestJSONRoundTrips(t *testing.T) {
	src := "fn through(text: Text) -> Text {\n    return Data::from_json(text).unwrap_or(Data::Null).to_json()\n}\n"

	for _, bc := range []bool{false, true} {
		bytecode = bc
		p := program(t, src, run.Config{})
		rng := rand.New(rand.NewSource(99))

		for i := range 400 {
			d := randomDoc(rng, 0)

			var messy, canonical strings.Builder

			d.write(&messy, rng, true)
			d.write(&canonical, rng, false)

			got, err := p.Call(context.Background(), "app", "through", messy.String())
			if err != nil || got != canonical.String() {
				t.Fatalf("bytecode=%v, document %d:\nread    %q\ngot     %v (%v)\nwanted  %s", bc, i, messy.String(), got, err, canonical.String())
			}

			// and what is written is read back as itself
			again, err := p.Call(context.Background(), "app", "through", canonical.String())
			if err != nil || again != canonical.String() {
				t.Fatalf("bytecode=%v, document %d: %s is written again as %v (%v)", bc, i, canonical.String(), again, err)
			}
		}
	}

	bytecode = false
}

func TestDataFromAndToGo(t *testing.T) {
	p := program(t, "fn f() {\n}\n", run.Config{})

	in := map[string]any{"b": []any{1, 2.5, "x", nil, true}, "a": map[string]any{"k": int64(7)}}

	d, err := p.DataFromGo(in)
	if err != nil {
		t.Fatal(err)
	}

	// the keys of a Go map are in the order of the keys
	if got := run.Format(d); !strings.Contains(got, "Map({") || strings.Index(got, `"a"`) > strings.Index(got, `"b"`) {
		t.Errorf("%s", got)
	}

	back, err := run.DataToGo(d)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]any{"b": []any{int64(1), 2.5, "x", nil, true}, "a": map[string]any{"k": int64(7)}}
	if !reflect.DeepEqual(back, want) {
		t.Errorf("%#v", back)
	}

	if _, err := p.DataFromGo(struct{}{}); err == nil {
		t.Error("a struct is not data")
	}

	if _, err := run.DataToGo(int64(1)); err == nil {
		t.Error("an Int is not a Data")
	}
}

// A document that nests too deep is not one, and it is not a stack that runs out.
func TestJSONNestingIsLimited(t *testing.T) {
	src := "fn ok(text: Text) -> Bool {\n    return Data::from_json(text).is_ok()\n}\n"

	for _, bc := range []bool{false, true} {
		bytecode = bc
		p := program(t, src, run.Config{})

		for depth, want := range map[int]bool{10: true, 900: true, 1500: false, 100000: false} {
			text := strings.Repeat("[", depth) + strings.Repeat("]", depth)

			got, err := p.Call(context.Background(), "app", "ok", text)
			if err != nil || got != want {
				t.Errorf("bytecode=%v, depth %d: %v, %v", bc, depth, got, err)
			}
		}
	}

	bytecode = false
}
