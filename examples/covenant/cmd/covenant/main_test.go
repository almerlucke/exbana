package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/covenant/tool"
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
	if code != 1 || !strings.Contains(errs, "testdata/broken.cov:2:12: expected Int, found Text") {
		t.Errorf("broken: %d %q", code, errs)
	}

	code, _, errs = cmd("test", "testdata/failing.cov")
	if code != 1 || !strings.Contains(errs, "testdata/failing.cov:3: example in double fails: it is false") {
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
	if code != 1 || !strings.Contains(errs, "fault (overflow) at testdata/demo.cov:32:12") {
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

func TestMainAndItsArguments(t *testing.T) {
	for _, flags := range [][]string{nil, {"-bytecode"}} {
		run := func(args ...string) (int, string, string) {
			return cmd(append(append([]string{"run"}, flags...), args...)...)
		}

		// the arguments are the ones after the files, and the number of them is the exit code
		code, out, errs := run("testdata/greeter.cov", "ann", "bob")
		if code != 2 || out != "hello, ann\nhello, bob\n" || errs != "" {
			t.Errorf("%v: %d %q %q", flags, code, out, errs)
		}

		// -- says that what follows is not a function to call
		code, out, _ = run("testdata/greeter.cov", "--", "a::b")
		if code != 1 || out != "hello, a::b\n" {
			t.Errorf("%v: %d %q", flags, code, out)
		}

		code, out, _ = run("testdata/greeter.cov")
		if code != 0 || out != "" {
			t.Errorf("%v: %d %q", flags, code, out)
		}

		// an Err is the standard error and the exit code 1
		code, out, errs = run("testdata/oops.cov")
		if code != 1 || out != "" || errs != "error: it went wrong\n" {
			t.Errorf("%v: %d %q %q", flags, code, out, errs)
		}

		// a program that has no main says so, and a main with arguments that it does not take
		if code, _, errs = run("testdata/spin.cov"); code != 2 || !strings.Contains(errs, "has no main") {
			t.Errorf("%v: %d %q", flags, code, errs)
		}

		if code, _, errs = run("testdata/oops.cov", "extra"); code != 2 || !strings.Contains(errs, "main takes no arguments") {
			t.Errorf("%v: %d %q", flags, code, errs)
		}
	}
}

func TestPermissions(t *testing.T) {
	// nothing is given, and main says what it needs
	code, out, errs := cmd("run", "testdata/needs.cov")
	if code != 1 || out != "" {
		t.Errorf("%d %q", code, out)
	}

	for _, want := range []string{"needs::main needs permissions", "fs::read", "-allow-read=DIR", "net::fetch", "-allow-net=HOST", "-allow-signature"} {
		if !strings.Contains(errs, want) {
			t.Errorf("no %q in %q", want, errs)
		}
	}

	// only some of it is given: what is left is what is said
	code, _, errs = cmd("run", "-allow-read=.", "testdata/needs.cov")
	if code != 1 || strings.Contains(errs, "fs::read") || !strings.Contains(errs, "net::fetch") {
		t.Errorf("%d %q", code, errs)
	}

	// and when it is all given, it runs
	if code, _, errs = cmd("run", "-allow-read=.", "-allow-net=127.0.0.1", "-allow-private-net", "testdata/needs.cov"); code != 0 {
		t.Errorf("%d %q", code, errs)
	}

	if code, _, errs = cmd("run", "-allow-all", "testdata/needs.cov"); code != 0 {
		t.Errorf("-allow-all: %d %q", code, errs)
	}

	// -allow-signature grants what has nothing to scope, and not the rest
	if code, _, errs = cmd("run", "-allow-signature", "testdata/needs.cov"); code != 1 || !strings.Contains(errs, "-allow-read=DIR") {
		t.Errorf("-allow-signature: %d %q", code, errs)
	}

	code, out, errs = cmd("run", "-allow-signature", "testdata/timer.cov")
	if code != 0 || !regexp.MustCompile(`^\d{13}\n5\n$`).MatchString(out) {
		t.Errorf("clock: %d %q %q", code, out, errs)
	}

	if code, _, errs = cmd("run", "-allow-time", "testdata/timer.cov"); code != 1 || !strings.Contains(errs, "\n  random") || strings.Contains(errs, "\n  time") {
		t.Errorf("only time: %d %q", code, errs)
	}

	// the same for a function that is named
	if code, _, errs = cmd("run", "testdata/needs.cov", "needs::main"); code != 1 || !strings.Contains(errs, "-allow-read=DIR") {
		t.Errorf("named: %d %q", code, errs)
	}
}

func TestDescribeAndCall(t *testing.T) {
	code, out, errs := cmd("describe", "testdata/notes.cov")
	if code != 0 {
		t.Fatalf("%d %q", code, errs)
	}

	var m tool.Manifest
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	byName := map[string]tool.Tool{}
	for _, tl := range m.Tools {
		byName[tl.Name] = tl
	}

	if len(m.Tools) != 3 || byName["notes.add"].Description != "Adds two numbers." || !reflect.DeepEqual(byName["notes.read_note"].Effects, []string{"fs::read"}) {
		t.Errorf("%s", out)
	}

	if !strings.HasPrefix(byName["notes.read_note"].Description, "Reads a note from a file.\nIt gives an error") {
		t.Errorf("%q", byName["notes.read_note"].Description)
	}

	if len(m.Skipped) != 1 || m.Skipped[0].Name != "notes.identity" {
		t.Errorf("%v", m.Skipped)
	}

	// call: JSON in and JSON out
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"notes::add", `{"a": 20, "b": 22}`}, "42\n"},
		{[]string{"notes::add", `[1, 2]`}, "3\n"},
		{[]string{"notes::shout", `{"text": "hi"}`}, "\"HI!\"\n"},
	} {
		code, out, errs := cmd(append([]string{"call", "testdata/notes.cov"}, c.args...)...)
		if code != 0 || out != c.want {
			t.Errorf("%v: %d %q %q", c.args, code, out, errs)
		}
	}

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"notes::add", `{"a": 1}`}, "the argument b is missing"},
		{[]string{"notes::add", `{"a": "x", "b": 1}`}, "a: expected an integer, found a string"},
		{[]string{"notes::hidden", `{}`}, "is not public"},
		{[]string{"notes::identity", `{"x": 1}`}, "is not a tool: it is generic"},
		{[]string{"notes::nothing", `{}`}, "has no function nothing"},
		{[]string{"notes"}, "is not module::function"},
	} {
		code, _, errs := cmd(append([]string{"call", "testdata/notes.cov"}, c.args...)...)
		if code == 0 || !strings.Contains(errs, c.want) {
			t.Errorf("%v: %d %q, want %q", c.args, code, errs, c.want)
		}
	}

	// the arguments from the standard input
	stdin = strings.NewReader(`{"a": 5, "b": 6}`)

	defer func() { stdin = os.Stdin }()

	if code, out, _ := cmd("call", "testdata/notes.cov", "notes::add", "-"); code != 0 || out != "11\n" {
		t.Errorf("stdin: %d %q", code, out)
	}
}

func TestACallOfATool(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a note"), 0o644); err != nil {
		t.Fatal(err)
	}

	read := func(flags []string, path string) (int, string, string) {
		args := append(append([]string{"call"}, flags...), "testdata/notes.cov", "notes::read_note", `{"path": "`+path+`"}`)
		return cmd(args...)
	}

	// without a permission it is not called
	code, _, errs := read(nil, filepath.Join(dir, "a.txt"))
	if code != 1 || !strings.Contains(errs, "-allow-read=DIR") {
		t.Errorf("%d %q", code, errs)
	}

	// with one it is, for what is inside the directory and not for what is outside
	flags := []string{"-allow-read=" + dir}

	if code, out, errs := read(flags, filepath.Join(dir, "a.txt")); code != 0 || out != `{"ok":"a note"}`+"\n" {
		t.Errorf("%d %q %q", code, out, errs)
	}

	if code, out, _ := read(flags, filepath.Join(dir, "none.txt")); code != 0 || out != `{"err":"NotFound"}`+"\n" {
		t.Errorf("%d %q", code, out)
	}

	if code, out, _ := read(flags, "/etc/hostname"); code != 0 || out != `{"err":"Denied"}`+"\n" {
		t.Errorf("%d %q", code, out)
	}
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for path, text := range files {
		full := filepath.Join(root, path)

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const myapp = `use io
use myapp::util

fn main(args: List<Text>) with io::print -> Int {
    io::print(greeting(args))
    return util::double(21) - 42 + args.length()
}
`

func TestADirectoryIsAModule(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "myapp")

	writeFiles(t, root, map[string]string{
		"main.cov": myapp,
		// private to the module, and used by the file above
		"greet.cov":     "// the greeting\nfn greeting(args: List<Text>) -> Text {\n    return \"hello \" + args.fold(\"\", fn(a, b) => a + b)\n}\n",
		"util/util.cov": "pub fn double(n: Int) -> Int {\n    example double(2) == 4\n    return n * 2\n}\n",
		"notes.txt":     "not code",
	})

	for _, flags := range [][]string{nil, {"-bytecode"}} {
		code, out, errs := cmd(append(append([]string{"run"}, flags...), root, "ann", "bob")...)
		if code != 2 || out != "hello annbob\n" || errs != "" {
			t.Errorf("%v: %d %q %q", flags, code, out, errs)
		}
	}

	// an argument that is a directory is an argument, once the program is given
	if code, out, _ := cmd("run", root, "."); code != 1 || out != "hello .\n" {
		t.Errorf("a directory as an argument: %d %q", code, out)
	}

	if code, out, errs := cmd("check", root); code != 0 || out != "2 modules: no problems\n" {
		t.Errorf("check: %d %q %q", code, out, errs)
	}

	if code, out, errs := cmd("test", root); code != 0 || out != "1 examples, 0 fail\n" {
		t.Errorf("test: %d %q %q", code, out, errs)
	}

	// the public functions of all the modules are tools
	code, out, _ := cmd("describe", root)

	var m tool.Manifest
	if err := json.Unmarshal([]byte(out), &m); err != nil || code != 0 || len(m.Tools) != 1 || m.Tools[0].Name != "myapp::util.double" {
		t.Errorf("describe: %d %v %q", code, err, out)
	}

	if code, out, errs := cmd("call", root, "myapp::util::double", `{"n": 4}`); code != 0 || out != "8\n" {
		t.Errorf("call: %d %q %q", code, out, errs)
	}

	// a problem is in the file it is in, with the path it was given
	writeFiles(t, root, map[string]string{"greet.cov": "// the greeting\nfn greeting(args: List<Text>) -> Text {\n    return 1\n}\n"})

	code, _, errs := cmd("check", root)
	if code != 1 || errs != filepath.Join(root, "greet.cov")+":3:12: expected Text, found Int\n" {
		t.Errorf("a problem: %d %q", code, errs)
	}
}

func TestLibDirectories(t *testing.T) {
	base := t.TempDir()

	writeFiles(t, filepath.Join(base, "app"), map[string]string{
		"app.cov": "use io\nuse mathx\n\nfn main() with io::print {\n    io::print(mathx::triple(14).to_text())\n}\n",
	})

	writeFiles(t, filepath.Join(base, "libs", "mathx"), map[string]string{
		"a.cov": "pub fn triple(n: Int) -> Int {\n    return n * helper()\n}\n",
		"b.cov": "fn helper() -> Int {\n    return 3\n}\n",
	})

	if code, out, errs := cmd("run", "-lib", filepath.Join(base, "libs", "mathx"), filepath.Join(base, "app")); code != 0 || out != "42\n" {
		t.Errorf("%d %q %q", code, out, errs)
	}

	// without it the module is not there
	if code, _, errs := cmd("run", filepath.Join(base, "app")); code != 1 || !strings.Contains(errs, "there is no module mathx") {
		t.Errorf("%d %q", code, errs)
	}

	// and a module that is in both is a problem
	writeFiles(t, filepath.Join(base, "app2", "mathx"), map[string]string{"x.cov": "fn x() {\n}\n"})

	if code, _, errs := cmd("check", "-lib", filepath.Join(base, "libs", "mathx"), filepath.Join(base, "app2", "mathx")); code != 1 || !strings.Contains(errs, "the module mathx is there twice") {
		t.Errorf("%d %q", code, errs)
	}
}

func TestADirectoryThatIsNotAProgram(t *testing.T) {
	base := t.TempDir()

	writeFiles(t, filepath.Join(base, "my-app"), map[string]string{"main.cov": "fn main() {\n}\n"})
	writeFiles(t, filepath.Join(base, "empty"), map[string]string{"notes.txt": "x"})
	writeFiles(t, filepath.Join(base, "nomain"), map[string]string{"lib.cov": "pub fn f() {\n}\n"})

	for dir, want := range map[string]string{
		"my-app": "my-app is not a name that a module can have",
		"empty":  "there are no .cov files in",
	} {
		if code, _, errs := cmd("run", filepath.Join(base, dir)); code != 1 || !strings.Contains(errs, want) {
			t.Errorf("%s: %d %q", dir, code, errs)
		}
	}

	if code, _, errs := cmd("run", filepath.Join(base, "nomain")); code != 2 || !strings.Contains(errs, "module nomain has no main") {
		t.Errorf("no main: %d %q", code, errs)
	}
}

func TestAProgramUnderAnotherName(t *testing.T) {
	base := t.TempDir()

	// a checkout that is called what a module can not be called
	root := filepath.Join(base, "myapp-main")
	writeFiles(t, root, map[string]string{
		"main.cov":      "use io\nuse app::util\n\nfn main(args: List<Text>) with io::print -> Int {\n    io::print(\"twice \" + util::double(21).to_text())\n    return args.length()\n}\n",
		"util/util.cov": "pub fn double(n: Int) -> Int {\n    return n * 2\n}\n",
	})

	if code, _, errs := cmd("run", root); code != 1 || !strings.Contains(errs, "myapp-main is not a name that a module can have") {
		t.Errorf("without a name: %d %q", code, errs)
	}

	for _, flags := range [][]string{nil, {"-bytecode"}} {
		code, out, errs := cmd(append(append([]string{"run"}, flags...), "app="+root, "x", "y")...)
		if code != 2 || out != "twice 42\n" || errs != "" {
			t.Errorf("%v: %d %q %q", flags, code, out, errs)
		}
	}

	// the function to call is by the name it was given
	if code, out, errs := cmd("call", "app="+root, "app::util::double", `{"n": 4}`); code != 0 || out != "8\n" {
		t.Errorf("call: %d %q %q", code, out, errs)
	}

	// a library under the name it is used by
	writeFiles(t, filepath.Join(base, "somewhere", "else"), map[string]string{"m.cov": "pub fn triple(n: Int) -> Int {\n    return n * 3\n}\n"})
	writeFiles(t, filepath.Join(base, "app"), map[string]string{"app.cov": "use io\nuse mathx\n\nfn main() with io::print {\n    io::print(mathx::triple(5).to_text())\n}\n"})

	lib := filepath.Join(base, "somewhere", "else")

	if code, out, errs := cmd("run", "-lib", "mathx="+lib, filepath.Join(base, "app")); code != 0 || out != "15\n" {
		t.Errorf("-lib name=dir: %d %q %q", code, out, errs)
	}

	// a name that is not one
	if code, _, errs := cmd("run", "bad-name="+root); code != 1 || !strings.Contains(errs, "bad-name is not a name that a module can have") {
		t.Errorf("a bad name: %d %q", code, errs)
	}

	// a file under a name
	writeFiles(t, base, map[string]string{"my-script.cov": "use io\n\nfn main() with io::print {\n    io::print(\"script\")\n}\n"})

	if code, out, errs := cmd("run", "script="+filepath.Join(base, "my-script.cov")); code != 0 || out != "script\n" {
		t.Errorf("a file under a name: %d %q %q", code, out, errs)
	}

	// an argument of the program with = in it is an argument
	writeFiles(t, base, map[string]string{"g.cov": "use io\n\nfn main(args: List<Text>) with io::print {\n    io::print(args.fold(\"\", fn(a, b) => a + b))\n}\n"})

	if code, out, _ := cmd("run", filepath.Join(base, "g.cov"), "a=b", "c=d"); code != 0 || out != "a=bc=d\n" {
		t.Errorf("an argument with =: %d %q", code, out)
	}
}
