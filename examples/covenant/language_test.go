package covenant_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
	"github.com/almerlucke/exbana/v3/examples/covenant/format"
	"github.com/almerlucke/exbana/v3/examples/covenant/run"
	"github.com/almerlucke/exbana/v3/examples/covenant/std"
	"github.com/almerlucke/exbana/v3/examples/covenant/token"
	"github.com/almerlucke/exbana/v3/examples/covenant/tool"
)

// LANGUAGE.md describes the language with examples, and the examples are true: this checks every program in it, and runs the
// examples of the programs, both ways. The blocks that are code are marked by what they are:
//
//	covenant                 a module that has to check, and whose examples have to be true
//	covenant-error           a module that has to have a problem; its first line is "// error: " and a part of the message
//	covenant-modules         modules, each after a line "--- name", that have to check together
//	covenant-modules-error   modules that have to have a problem, as for covenant-error
//	covenant-prelude         the prelude, which has to be the file check/prelude.cov
//	covenant-format          a source, a line "--- formatted", and what covenant fmt makes of it
//	json-for-previous        the manifest of the covenant block just before it, exactly as covenant describe writes it

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

// the examples use the standard library: io, fs, net, env, process, clock and random are the modules of the host
var docConfig = check.Config{Native: std.Natives()}

// modulesOf is the modules in a block: the text itself is the module doc, or it is cut at the lines "--- module file.cov", and the
// file is the module name with .cov if it is not said.
func modulesOf(b block, multiple bool) map[string][]check.Source {
	if !multiple {
		return map[string][]check.Source{"doc": {{Name: "doc.cov", Text: b.text}}}
	}

	mods := map[string][]check.Source{}

	var module, file string

	index := -1

	for _, l := range strings.Split(b.text, "\n") {
		if header, ok := strings.CutPrefix(l, "--- "); ok {
			fields := strings.Fields(header)

			module, file = fields[0], fields[0]+".cov"
			if len(fields) > 1 {
				file = fields[1]
			}

			mods[module] = append(mods[module], check.Source{Name: module + "/" + file})
			index = len(mods[module]) - 1

			continue
		}

		if module != "" {
			mods[module][index].Text += l + "\n"
		}
	}

	return mods
}

func TestLanguageExamplesAreTrue(t *testing.T) {
	checked, examples, manifests := 0, 0, 0

	var previous *check.Result

	for _, b := range blocks(t) {
		var multiple, wantError bool

		switch b.kind {
		case "json-for-previous":
			if previous == nil {
				t.Errorf("line %d: a manifest, and no program before it", b.line)
				continue
			}

			m, err := tool.Describe(previous)
			if err != nil {
				t.Errorf("line %d: %v", b.line, err)
				continue
			}

			got, _ := m.JSON()
			if strings.TrimSpace(b.text) != string(got) {
				t.Errorf("line %d: the manifest is not what covenant describe writes:\n%s", b.line, got)
			}

			manifests++

			continue
		case "covenant-format":
			before, after, ok := strings.Cut(b.text, "--- formatted\n")
			if !ok {
				t.Errorf("line %d: an example of the formatter has a line --- formatted", b.line)
				continue
			}

			got, err := format.Source(before)
			if err != nil {
				t.Errorf("line %d: %v", b.line, err)
			} else if got != after {
				t.Errorf("line %d: the formatter does not make this of it:\n%s", b.line, got)
			}

			continue
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
		res := check.CheckModules(docConfig, mods)

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

		previous = res

		for _, bytecode := range []bool{false, true} {
			// the host has a policy that allows nothing: the examples do not call the host, and they could not
			host := std.New(std.Policy{})

			prog, err := run.New(res, run.Config{Host: host.Host(), Bytecode: bytecode})
			if err != nil {
				t.Errorf("line %d: %v", b.line, err)
				continue
			}

			host.Bind(prog)

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

	if checked < 35 || examples < 65 || manifests < 1 {
		t.Errorf("only %d programs, %d examples and %d manifests were checked", checked, examples, manifests)
	}

	t.Logf("%d programs checked, %d examples are true, and %d manifests are as written", checked, examples, manifests)
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
