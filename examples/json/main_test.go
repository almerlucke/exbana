package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseMatchesEncodingJSON(t *testing.T) {
	inputs := []string{
		`null`, `true`, `false`, `0`, `-12.5e3`, `"plain"`, `"esc \" \\ \/ \n \t A"`,
		`[]`, `[ ]`, `[1, 2, 3]`, `{}`, `{ }`,
		`{ "foo": "bar⌘34", "test": 2.3, "hallo": [1, 2, 3, "test", null, true, false, [ ]] }`,
		"  \n{\"a\" : { \"b\" : [ { \"c\" : null } ] } }\t ",
	}

	for _, in := range inputs {
		got, err := Parse(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}

		var want any
		if err := json.Unmarshal([]byte(in), &want); err != nil {
			t.Fatalf("%q: %v", in, err)
		}

		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %#v, want %#v", in, got, want)
		}
	}
}

func TestParseRejectsInvalidJSON(t *testing.T) {
	for _, in := range []string{``, `[1,`, `{"a" 1}`, `[1 2]`, `tru`, `"unterminated`, `{"a":1} x`, `01`} {
		if v, err := Parse(in); err == nil {
			t.Errorf("%q: expected an error, got %#v", in, v)
		}
	}
}

func TestSyntaxErrorsSayWhereAndWhat(t *testing.T) {
	_, err := Parse(`{"a": [1, 2,, 3]}`)
	if err == nil {
		t.Fatal("expected an error")
	}

	t.Log(err)
	msg := err.Error()
	for _, want := range []string{"1:13", "unexpected ','", "expected"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q does not contain %q", msg, want)
		}
	}
}
