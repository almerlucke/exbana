package run_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
	"github.com/almerlucke/exbana/v3/examples/covenant/run"
)

func twoFiles(t *testing.T, bc bool) *run.Program {
	t.Helper()

	res := check.CheckModules(check.Config{}, map[string][]check.Source{"app": {
		{Name: "app/a.cov", Text: "fn first() -> Int {\n    return second(0)\n}\n\nfn quotient(n: Int) -> Int {\n    return third(n)\n}\n"},
		{Name: "app/b.cov", Text: "// the file with the others\n\nfn second(n: Int) -> Int {\n    requires n > 0\n    return n\n}\n\nfn third(n: Int) -> Int {\n    example third(5) == 2\n    example third(2) == 9\n    return 10 / n\n}\n"},
	}})
	if !res.OK() {
		t.Fatal(res.Error())
	}

	p, err := run.New(res, run.Config{Bytecode: bc})
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestFaultsAreInTheFileTheyAreIn(t *testing.T) {
	var faults [2][2]*run.Fault

	for i, bc := range []bool{false, true} {
		p := twoFiles(t, bc)

		for j, c := range []struct {
			fn   string
			args []run.Value
		}{{"first", nil}, {"quotient", []run.Value{int64(0)}}} {
			_, err := p.Call(context.Background(), "app", c.fn, c.args...)
			if !errors.As(err, &faults[i][j]) {
				t.Fatalf("bytecode=%v %s: %v", bc, c.fn, err)
			}
		}

		// a promise that is false is in the file of the function that makes it, and so is a division
		f := faults[i][0]
		if f.Kind != "requires" || f.File != "app/b.cov" || f.Line != 4 || f.Col != 14 || f.Module != "app" {
			t.Errorf("bytecode=%v: %+v", bc, f)
		}

		if !reflect.DeepEqual(f.Stack, []string{"app::second", "app::first"}) {
			t.Errorf("stack %v", f.Stack)
		}

		f = faults[i][1]
		if f.Kind != "division" || f.File != "app/b.cov" || f.Line != 11 || f.Col != 12 {
			t.Errorf("bytecode=%v: %+v", bc, f)
		}

		if !strings.Contains(f.Error(), "at app/b.cov:11:12") {
			t.Errorf("%s", f.Error())
		}
	}

	if !reflect.DeepEqual(faults[0], faults[1]) {
		t.Errorf("the tree and the bytecode differ:\n%+v %+v\n%+v %+v", faults[0][0], faults[0][1], faults[1][0], faults[1][1])
	}
}

func TestExamplesAreInTheFileTheyAreIn(t *testing.T) {
	for _, bc := range []bool{false, true} {
		got := twoFiles(t, bc).Examples(context.Background())
		if len(got) != 2 {
			t.Fatalf("%d examples", len(got))
		}

		for i, want := range []struct {
			line int
			ok   bool
		}{{9, true}, {10, false}} {
			if got[i].File != "app/b.cov" || got[i].Line != want.line || (got[i].Err == nil) != want.ok || got[i].Function != "third" {
				t.Errorf("bytecode=%v, example %d: %+v", bc, i, got[i])
			}
		}
	}
}

func TestDisassemblyHasTheLinesOfTheFile(t *testing.T) {
	out, err := twoFiles(t, true).Disassemble("app", "third")
	if err != nil {
		t.Fatal(err)
	}

	// the lines are the ones of b.cov: the function is at line 8, and the division at line 11
	if !strings.Contains(out, "  11  ") || strings.Contains(out, "  26  ") {
		t.Errorf("%s", out)
	}
}
