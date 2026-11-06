package covenant_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
	"github.com/almerlucke/exbana/v3/examples/covenant/run"
	"github.com/almerlucke/exbana/v3/examples/covenant/token"
)

// LANGUAGE.md describes the language with examples, and the examples are true: this checks every program in it, and runs the
// examples of the programs, both ways. The blocks that are code are marked by what they are:
//
//	covenant                 a module that has to check, and whose examples have to be true
//	covenant-error           a module that has to have a problem; its first line is "// error: " and a part of the message
//	covenant-modules         modules, each after a line "--- name", that have to check together
//	covenant-modules-error   modules that have to have a problem, as for covenant-error
//	covenant-prelude         the prelude, which has to be the file check/prelude.cov

type block struct {
	kind string
	line int
	text string
}

func blocks(t *testing.T) []block {
	t.Helper()

	data, err := os.ReadFile("LANGUAGE.md")
	if err != nil {
		t.Fatal(err)
	}

	var out []block

	lines := strings.Split(string(data), "\n")

	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "```") || len(lines[i]) == 3 {
			continue
		}

		b := block{kind: strings.TrimPrefix(lines[i], "```"), line: i + 1}

		for i++; i < len(lines) && lines[i] != "```"; i++ {
			b.text += lines[i] + "\n"
		}

		out = append(out, b)
	}

	return out
}

// the modules of the host that the examples use: io is the one the command has, and the others are what a host can add
var docConfig = check.Config{
	Effects: []string{"time", "random"},
	Native: map[string]string{
		"io":    "trait Functions {\n    fn print(text: Text) with io::print\n}\n",
		"clock": "trait Functions {\n    fn now() with time -> Int\n}\n",
		"fs": `struct Path {}
struct IoError {}

trait Functions {
    fn read_all(path: Path) with fs::read -> Result<Text, IoError>
    fn write_all(path: Path, data: Text) with fs::write -> Result<Unit, IoError>
}
`,
	},
}

func stubs(prog **run.Program) map[string]run.HostFunc {
	return map[string]run.HostFunc{
		"io::print":     func(ctx context.Context, a []run.Value) (run.Value, error) { return run.Unit{}, nil },
		"clock::now":    func(ctx context.Context, a []run.Value) (run.Value, error) { return int64(0), nil },
		"fs::read_all":  func(ctx context.Context, a []run.Value) (run.Value, error) { return (*prog).Ok("contents"), nil },
		"fs::write_all": func(ctx context.Context, a []run.Value) (run.Value, error) { return (*prog).Ok(run.Unit{}), nil },
	}
}

// modulesOf is the modules in a block: the text itself is the module doc, or it is cut at the lines "--- name".
func modulesOf(b block, multiple bool) map[string]string {
	if !multiple {
		return map[string]string{"doc": b.text}
	}

	mods := map[string]string{}
	name := ""

	for _, l := range strings.Split(b.text, "\n") {
		if strings.HasPrefix(l, "--- ") {
			name = strings.TrimPrefix(l, "--- ")
			continue
		}

		if name != "" {
			mods[name] += l + "\n"
		}
	}

	return mods
}

func TestLanguageExamplesAreTrue(t *testing.T) {
	checked, examples := 0, 0

	for _, b := range blocks(t) {
		var multiple, wantError bool

		switch b.kind {
		case "covenant":
		case "covenant-error":
			wantError = true
		case "covenant-modules":
			multiple = true
		case "covenant-modules-error":
			multiple, wantError = true, true
		default:
			continue
		}

		checked++

		mods := modulesOf(b, multiple)
		res := check.Check(docConfig, mods)

		if wantError {
			first, _, _ := strings.Cut(b.text, "\n")

			want, ok := strings.CutPrefix(first, "// error: ")
			if !ok {
				t.Errorf("line %d: an example of an error has to start with // error: and a part of the message", b.line)
				continue
			}

			if res.OK() {
				t.Errorf("line %d: no problem is found, and %q is said to be one:\n%s", b.line, want, b.text)
			} else if !strings.Contains(res.Error().Error(), want) {
				t.Errorf("line %d: the problem is not %q but:\n%v", b.line, want, res.Error())
			}

			continue
		}

		if !res.OK() {
			t.Errorf("line %d: the example does not check:\n%v\n%s", b.line, res.Error(), b.text)
			continue
		}

		for _, bytecode := range []bool{false, true} {
			var prog *run.Program

			prog, err := run.New(res, run.Config{Host: stubs(&prog), Bytecode: bytecode})
			if err != nil {
				t.Errorf("line %d: %v", b.line, err)
				continue
			}

			// the stubs call back into the program that was just made
			prog, _ = run.New(res, run.Config{Host: stubs(&prog), Bytecode: bytecode})

			for _, ex := range prog.Examples(context.Background()) {
				if bytecode {
					examples++
				}

				if ex.Err != nil {
					t.Errorf("line %d (bytecode=%v): the example %q in %s is not true: %v", b.line+ex.Line-1, bytecode, ex.Text, ex.Function, ex.Err)
				}
			}
		}
	}

	if checked < 30 || examples < 60 {
		t.Errorf("only %d programs and %d examples were checked", checked, examples)
	}

	t.Logf("%d programs checked, and %d examples are true", checked, examples)
}

func TestLanguagePreludeIsThePrelude(t *testing.T) {
	prelude, err := os.ReadFile("check/prelude.cov")
	if err != nil {
		t.Fatal(err)
	}

	found := false

	for _, b := range blocks(t) {
		if b.kind == "covenant-prelude" {
			found = true

			if strings.TrimSpace(b.text) != strings.TrimSpace(string(prelude)) {
				t.Errorf("the prelude in LANGUAGE.md is not check/prelude.cov: copy it again")
			}
		}
	}

	if !found {
		t.Error("LANGUAGE.md has no prelude")
	}
}

func TestLanguageNamesTheKeywords(t *testing.T) {
	data, _ := os.ReadFile("LANGUAGE.md")

	for _, k := range token.Keywords() {
		if !strings.Contains(string(data), "`"+k+"`") {
			t.Errorf("the keyword %s is not in LANGUAGE.md", k)
		}
	}
}

func TestLanguageLinksGoSomewhere(t *testing.T) {
	data, _ := os.ReadFile("LANGUAGE.md")
	text := string(data)

	slug := func(h string) string {
		h = strings.ToLower(h)
		h = regexp.MustCompile("[^a-z0-9 -]").ReplaceAllString(h, "")

		return strings.ReplaceAll(h, " ", "-")
	}

	anchors := map[string]bool{}

	for _, l := range strings.Split(text, "\n") {
		if h, ok := strings.CutPrefix(l, "## "); ok {
			anchors[slug(h)] = true
		}
	}

	for _, m := range regexp.MustCompile(`\]\(#([^)]+)\)`).FindAllStringSubmatch(text, -1) {
		if !anchors[m[1]] {
			t.Errorf("a link to #%s, and there is no such heading", m[1])
		}
	}
}

// The way to run a program from Go that LANGUAGE.md shows.
func TestLanguageGoExampleWorks(t *testing.T) {
	source := "fn double(n: Int) -> Int {\n    return n * 2\n}\n"

	res := check.Check(check.Config{}, map[string]string{"app": source})
	if !res.OK() {
		t.Fatal(res.Error())
	}

	prog, err := run.New(res, run.Config{Bytecode: true, Grant: []string{"fs::read"}})
	if err != nil {
		t.Fatal(err)
	}

	v, err := prog.Call(context.Background(), "app", "double", int64(21))
	if err != nil || run.Format(v) != "42" {
		t.Errorf("%v, %v", v, err)
	}
}
