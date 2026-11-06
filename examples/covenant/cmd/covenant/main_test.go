package main

import (
	"bytes"
	"strings"
	"testing"
)

func cmd(args ...string) (code int, out, errs string) {
	var o, e bytes.Buffer

	code = command(args, &o, &e)

	return code, o.String(), e.String()
}

func TestCheckRunTest(t *testing.T) {
	code, out, errs := cmd("check", "testdata/demo.cov")
	if code != 0 || !strings.Contains(out, "no problems") {
		t.Errorf("check: %d %q %q", code, out, errs)
	}

	code, out, errs = cmd("test", "testdata/demo.cov")
	if code != 0 || !strings.Contains(out, "2 examples, 0 fail") {
		t.Errorf("test: %d %q %q", code, out, errs)
	}

	code, out, errs = cmd("run", "testdata/demo.cov", "demo::main")
	if code != 0 || out != "hello, covenant\n12.0\n6.0\n3628800\n" {
		t.Errorf("run: %d %q %q", code, out, errs)
	}

	code, out, _ = cmd("run", "testdata/demo.cov", "demo::factorial", "6")
	if code != 0 || out != "720\n" {
		t.Errorf("factorial: %d %q", code, out)
	}
}

func TestFaultsAndProblemsAreReported(t *testing.T) {
	code, _, errs := cmd("run", "testdata/demo.cov", "demo::factorial", "-3")
	if code != 1 || !strings.Contains(errs, "fault (requires)") || !strings.Contains(errs, "n >= 0") {
		t.Errorf("fault: %d %q", code, errs)
	}

	code, _, errs = cmd("run", "testdata/demo.cov", "demo::factorial", "25")
	if code != 1 || !strings.Contains(errs, "fault (overflow)") || !strings.Contains(errs, "demo::factorial") {
		t.Errorf("overflow: %d %q", code, errs)
	}

	code, _, errs = cmd("run", "testdata/broken.cov", "broken::f")
	if code != 1 || !strings.Contains(errs, "broken:2:12: expected Int, found Text") {
		t.Errorf("broken: %d %q", code, errs)
	}

	code, _, errs = cmd("test", "testdata/failing.cov")
	if code != 1 || !strings.Contains(errs, "failing:3: example in double fails: it is false") {
		t.Errorf("failing: %d %q", code, errs)
	}

	if code, _, errs = cmd("run", "testdata/demo.cov", "demo::nothing"); code != 1 || !strings.Contains(errs, "has no nothing") {
		t.Errorf("unknown: %d %q", code, errs)
	}

	if code, _, _ = cmd(); code != 2 {
		t.Errorf("no arguments: %d", code)
	}
}

func TestSpinningIsEnded(t *testing.T) {
	code, _, errs := cmd("run", "-max-steps", "5000", "testdata/spin.cov", "spin::spin")
	if code != 1 || !strings.Contains(errs, "limit (steps)") {
		t.Errorf("%d %q", code, errs)
	}

	code, _, errs = cmd("run", "-max-steps", "-1", "-timeout", "30ms", "testdata/spin.cov", "spin::spin")
	if code != 1 || !strings.Contains(errs, "limit (cancelled)") {
		t.Errorf("%d %q", code, errs)
	}
}

func TestBytecode(t *testing.T) {
	code, out, errs := cmd("run", "-bytecode", "testdata/demo.cov", "demo::main")
	if code != 0 || out != "hello, covenant\n12.0\n6.0\n3628800\n" {
		t.Errorf("run: %d %q %q", code, out, errs)
	}

	code, out, errs = cmd("test", "-bytecode", "testdata/demo.cov")
	if code != 0 || !strings.Contains(out, "2 examples, 0 fail") {
		t.Errorf("test: %d %q %q", code, out, errs)
	}

	code, _, errs = cmd("run", "-bytecode", "testdata/demo.cov", "demo::factorial", "25")
	if code != 1 || !strings.Contains(errs, "fault (overflow) at demo:32:12") {
		t.Errorf("overflow: %d %q", code, errs)
	}

	code, out, errs = cmd("dis", "testdata/demo.cov", "demo::factorial")
	if code != 0 || !strings.Contains(out, "func demo::factorial (params 1, locals 1, captures 0)") || !strings.Contains(out, "calldef") {
		t.Errorf("dis: %d %q %q", code, out, errs)
	}

	if code, _, _ = cmd("dis", "testdata/demo.cov"); code != 2 {
		t.Errorf("dis without a function: %d", code)
	}
}

func TestObjectsAndData(t *testing.T) {
	want := "3.0\n6.0\n{\"name\":\"covenant\",\"tags\":[1,2.5,null]}\na map of 2\n2\n"

	for _, flags := range [][]string{nil, {"-bytecode"}} {
		args := append(append([]string{"run"}, flags...), "testdata/objects.cov", "objects::main")

		code, out, errs := cmd(args...)
		if code != 0 || out != want {
			t.Errorf("%v: %d %q %q", flags, code, out, errs)
		}
	}
}
