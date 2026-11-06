package check

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func files(pairs ...string) []Source {
	out := make([]Source, 0, len(pairs)/2)

	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Source{Name: pairs[i], Text: pairs[i+1]})
	}

	return out
}

func checkFiles(mods map[string][]Source) *Result {
	return CheckModules(testConfig, mods)
}

// The files of a module are one set of names, whatever is private in one of them can be used in the others.
func TestFilesOfAModuleShareTheirNames(t *testing.T) {
	r := checkFiles(map[string][]Source{"shop": files(
		"shop/types.cov", `
struct Item { name: Text, price: Float }

enum Kind {
    Cheap
    Dear
}

trait Priced {
    fn price(self) -> Float
}
`,
		"shop/impls.cov", `
impl Priced for Item {
    fn price(self) -> Float {
        return self.price
    }
}

impl Item {
    pub fn kind(self) -> Kind {
        return classify(self.price)
    }
}
`,
		"shop/main.cov", `
use log

fn classify(price: Float) -> Kind {
    return match price > 10.0 {
        true => Kind::Dear
        false => Kind::Cheap
    }
}

pub fn total(items: List<Item>) -> Float {
    return items.fold(0.0, fn(sum, i) => sum + i.price)
}

pub fn describe(i: Item) with log::write {
    log::write(i.name)
}

fn needs_the_other(i: Item) -> Kind {
    return i.kind()
}
`)})

	if !r.OK() {
		t.Fatal(r.Error())
	}

	if got := len(r.Modules["shop"].Files); got != 3 {
		t.Errorf("%d files", got)
	}
}

// What a file of a module uses, the others can use: it is the module that uses it.
func TestUseIsForTheWholeModule(t *testing.T) {
	r := checkFiles(map[string][]Source{"m": files(
		"m/a.cov", "use log\n\nfn a() with log::write {\n    log::write(\"a\")\n}\n",
		"m/b.cov", "fn b() with log::write {\n    log::write(\"b\")\n}\n",
	)})

	if !r.OK() {
		t.Fatal(r.Error())
	}

	// two files that bring in the same thing are not a problem, and two that bring in different things by one name are
	r = checkFiles(map[string][]Source{"m": files(
		"m/a.cov", "use fs\nuse log\n\nfn a() {\n}\n",
		"m/b.cov", "use fs\n\nfn b() {\n}\n",
	)})

	if !r.OK() {
		t.Errorf("the same use in two files: %v", r.Error())
	}

	r = checkFiles(map[string][]Source{"m": files(
		"m/a.cov", "use fs as x\n\nfn a() {\n}\n",
		"m/b.cov", "use log as x\n\nfn b() {\n}\n",
	)})

	if r.OK() || !strings.Contains(r.Error().Error(), "x is already used for something else in this module") {
		t.Errorf("two things by one name: %v", r.Error())
	}
}

// A problem in a file is in that file, at the line and the column in it, and not in the text of the module.
func TestDiagnosticsAreInTheirFile(t *testing.T) {
	r := checkFiles(map[string][]Source{"m": files(
		"m/a.cov", "fn a() -> Int {\n    return 1\n}\n",
		"m/b.cov", "// a comment\n\nfn b() -> Int {\n    return \"x\"\n}\n",
		"m/c.cov", "fn c() -> Int {\n    let n = 1\n    return nope\n}\n",
	)})

	var got []string
	for _, d := range r.Diagnostics {
		got = append(got, d.String())
	}

	want := []string{
		"m/b.cov:4:12: expected Int, found Text",
		"m/c.cov:3:12: there is no nope",
	}

	// the message about nope may have a suggestion after it
	if len(got) != 2 || got[0] != want[0] || !strings.HasPrefix(got[1], want[1]) {
		t.Errorf("got  %q\nwant %q", got, want)
	}

	if r.Diagnostics[0].File != "m/b.cov" || r.Diagnostics[0].Module != "m" {
		t.Errorf("%+v", r.Diagnostics[0])
	}
}

func TestADeclarationTwiceAcrossFiles(t *testing.T) {
	// whichever order they are given in, the first is the one of the file that comes first by name
	for _, order := range [][]string{{"m/a.cov", "m/b.cov"}, {"m/b.cov", "m/a.cov"}} {
		srcs := map[string]string{"m/a.cov": "\nfn f() {\n}\n", "m/b.cov": "fn g() {\n}\n\nfn f() {\n}\n"}

		var fs []Source
		for _, n := range order {
			fs = append(fs, Source{n, srcs[n]})
		}

		r := checkFiles(map[string][]Source{"m": fs})

		if r.OK() || len(r.Diagnostics) != 1 {
			t.Fatalf("%v", r.Error())
		}

		d := r.Diagnostics[0]
		if d.File != "m/b.cov" || d.Line != 4 || !strings.Contains(d.Msg, "f is declared twice (the first is at m/a.cov:2)") {
			t.Errorf("order %v: %v", order, d)
		}
	}
}

func TestASyntaxErrorInOneFile(t *testing.T) {
	r := checkFiles(map[string][]Source{
		// a uses what is in b, which is not in the module: the module is left out, and that is not said again as a problem of a
		"m": files("m/a.cov", "fn a() -> Int {\n    return b()\n}\n", "m/b.cov", "fn b() -> Int {\n    let x = 1 let y = 2\n}\n"),
		"n": files("n/n.cov", "fn n() -> Int {\n    return \"x\"\n}\n"),
	})

	var got []string
	for _, d := range r.Diagnostics {
		got = append(got, d.File+":"+d.Code)
	}

	sort.Strings(got)

	// the module with the syntax error is left out, and the other module is still looked at
	if !reflect.DeepEqual(got, []string{"m/b.cov:syntax", "n/n.cov:type-mismatch"}) {
		t.Errorf("%q", got)
	}

	if r.Diagnostics[0].Line != 2 || r.Diagnostics[0].Col != 15 {
		t.Errorf("%+v", r.Diagnostics[0])
	}
}

func TestPrivateStaysInTheModule(t *testing.T) {
	r := checkFiles(map[string][]Source{
		"lib": files("lib/a.cov", "pub fn open() -> Int {\n    return hidden()\n}\n", "lib/b.cov", "fn hidden() -> Int {\n    return 1\n}\n"),
		"app": files("app/app.cov", "use lib\n\nfn f() -> Int {\n    return lib::hidden()\n}\n"),
	})

	if r.OK() || !strings.Contains(r.Error().Error(), "app/app.cov:4:") || !strings.Contains(r.Error().Error(), "hidden is not public") {
		t.Errorf("%v", r.Error())
	}
}

func TestLocate(t *testing.T) {
	r := checkFiles(map[string][]Source{"m": files("m/a.cov", "fn a() {\n}\n", "m/b.cov", "\n\nfn b() {\n}")})
	m := r.Modules["m"]

	if len(m.Files) != 2 || m.Files[0].Base != 0 || m.Files[1].Base != len("fn a() {\n}\n")+len(fileSeparator) {
		t.Fatalf("%+v", m.Files)
	}

	end := len("fn a() {\n}\n") // the end of the first file

	for offset, want := range map[int]string{
		0:                   "m/a.cov:1:1",
		3:                   "m/a.cov:1:4",
		end:                 "m/a.cov:3:1",
		m.Files[1].Base:     "m/b.cov:1:1",
		m.Files[1].Base + 2: "m/b.cov:3:1",
		len(m.Source):       "m/b.cov:4:2",
		len(m.Source) + 50:  "m/b.cov:4:2",
	} {
		file, line, col := m.Locate(offset)
		if got := file + ":" + itoa(line) + ":" + itoa(col); got != want {
			t.Errorf("offset %d is %s, want %s", offset, got, want)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// ---------------------------------------------------------------------------------------------------------------------------

func writeTree(t *testing.T, root string, tree map[string]string) {
	t.Helper()

	for path, text := range tree {
		full := filepath.Join(root, path)

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadDir(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "myapp")

	writeTree(t, root, map[string]string{
		"main.cov": "fn main() {\n}\n", "helpers.cov": "fn h() {\n}\n", "notes.txt": "not code",
		"util/util.cov": "pub fn u() {\n}\n", "util/deep/deep.cov": "pub fn d() {\n}\n",
		".hidden/x.cov": "fn x() {\n}\n", "_skipped/y.cov": "fn y() {\n}\n", ".dotfile.cov": "fn z() {\n}\n",
	})

	mods, name, err := LoadDir(root)
	if err != nil || name != "myapp" {
		t.Fatalf("%v, %q", err, name)
	}

	got := map[string][]string{}

	for m, srcs := range mods {
		for _, s := range srcs {
			got[m] = append(got[m], filepath.Base(s.Name))
		}

		sort.Strings(got[m])
	}

	want := map[string][]string{
		"myapp": {"helpers.cov", "main.cov"}, "myapp::util": {"util.cov"}, "myapp::util::deep": {"deep.cov"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}

	// the names of the files are the paths as they were given
	if n := mods["myapp::util"][0].Name; n != filepath.Join(root, "util", "util.cov") {
		t.Errorf("%s", n)
	}

	// and the program in it checks, with the modules using each other by those names
	writeTree(t, root, map[string]string{"main.cov": "use myapp::util\n\nfn main() -> Int {\n    return 1\n}\n"})

	mods, _, _ = LoadDir(root)
	if r := CheckModules(testConfig, mods); !r.OK() {
		t.Errorf("%v", r.Error())
	}
}

func TestLoadDirSaysWhatIsWrong(t *testing.T) {
	base := t.TempDir()

	for name, c := range map[string]struct {
		tree map[string]string
		dir  string
		want string
	}{
		"a name with a dash":        {map[string]string{"x.cov": "fn x() {\n}\n"}, "my-app", "my-app is not a name that a module can have"},
		"a name that is a keyword":  {map[string]string{"x.cov": "fn x() {\n}\n"}, "match", "match is not a name that a module can have"},
		"a name with a digit first": {map[string]string{"x.cov": "fn x() {\n}\n"}, "1app", "not a name"},
		"no files":                  {map[string]string{"notes.txt": "x"}, "empty", "there are no .cov files in"},
		"a directory with a dash":   {map[string]string{"sub-dir/x.cov": "fn x() {\n}\n"}, "good", "sub-dir is not a name"},
	} {
		root := filepath.Join(base, name, c.dir)
		writeTree(t, root, c.tree)

		if _, _, err := LoadDir(root); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}

	if _, _, err := LoadDir(filepath.Join(base, "nothing-here")); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("%v", err)
	}
}

func TestLoadFilesAndMerge(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.cov": "fn a() {\n}\n", "sub/b.cov": "fn b() {\n}\n", "bad-name.cov": "fn x() {\n}\n"})

	mods, err := LoadFiles([]string{filepath.Join(dir, "a.cov"), filepath.Join(dir, "sub", "b.cov")})
	if err != nil || len(mods) != 2 || mods["a"][0].Name != filepath.Join(dir, "a.cov") {
		t.Errorf("%v %v", mods, err)
	}

	if _, err := LoadFiles([]string{filepath.Join(dir, "bad-name.cov")}); err == nil {
		t.Error("a file with a name that is not a module name")
	}

	if _, err := LoadFiles([]string{filepath.Join(dir, "a.cov"), filepath.Join(dir, "a.cov")}); err == nil || !strings.Contains(err.Error(), "two files are the module a") {
		t.Errorf("%v", err)
	}

	if err := Merge(mods, map[string][]Source{"a": files("x.cov", "")}); err == nil || !strings.Contains(err.Error(), "the module a is there twice") {
		t.Errorf("%v", err)
	}

	if err := Merge(mods, map[string][]Source{"c": files("c.cov", "")}); err != nil || len(mods) != 3 {
		t.Errorf("%v", err)
	}
}

func TestSplitMount(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"real/a.cov": "fn a() {\n}\n", "a=b/c.cov": "fn c() {\n}\n", "x.cov": "fn x() {\n}\n"})

	real := filepath.Join(dir, "real")

	for arg, want := range map[string][2]string{
		real:                                 {"", real},                                // a path that is there
		"app=" + real:                        {"app", real},                             // a name for a path that is there
		"app::sub=" + real:                   {"app::sub", real},                        // a name can have ::
		"app=" + filepath.Join(dir, "nope"):  {"", "app=" + filepath.Join(dir, "nope")}, // nothing is there: it is the path, and it will say so
		"=" + real:                           {"", "=" + real},                          // no name
		filepath.Join(dir, "a=b"):            {"", filepath.Join(dir, "a=b")},           // a path with = in it that is there is that path
		"cov=" + filepath.Join(dir, "x.cov"): {"cov", filepath.Join(dir, "x.cov")},
	} {
		if name, path := SplitMount(arg); name != want[0] || path != want[1] {
			t.Errorf("%q is (%q, %q), want %q", arg, name, path, want)
		}
	}
}

func TestADirectoryUnderAnotherName(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "my-app-main") // not a name that a module can have

	writeTree(t, root, map[string]string{
		"main.cov": "use app::util\n\nfn main() -> Int {\n    return util::double(2)\n}\n", "util/util.cov": "pub fn double(n: Int) -> Int {\n    return n * 2\n}\n",
	})

	if _, _, err := LoadDir(root); err == nil || !strings.Contains(err.Error(), "my-app-main is not a name") {
		t.Errorf("without a name: %v", err)
	}

	mods, name, err := LoadDirAs(root, "app")
	if err != nil || name != "app" {
		t.Fatalf("%v, %q", err, name)
	}

	got := []string{}
	for m := range mods {
		got = append(got, m)
	}

	sort.Strings(got)

	if !reflect.DeepEqual(got, []string{"app", "app::util"}) {
		t.Errorf("%v", got)
	}

	if r := CheckModules(testConfig, mods); !r.OK() {
		t.Errorf("%v", r.Error())
	}

	// a name can have :: in it, and the modules in the directory are under it
	mods, name, err = LoadDirAs(root, "vendor::app")
	if err != nil || name != "vendor::app" || len(mods["vendor::app::util"]) != 1 {
		t.Errorf("%v %q %v", err, name, mods)
	}

	for _, bad := range []string{"my-app", "1app", "match", "app::", "::app", "app::my-sub", "a b"} {
		if _, _, err := LoadDirAs(root, bad); err == nil || !strings.Contains(err.Error(), "is not a name that a module can have") {
			t.Errorf("the name %q: %v", bad, err)
		}
	}

	// a directory in it that can not be a module is still a problem, whatever the root is called
	writeTree(t, root, map[string]string{"bad-dir/x.cov": "fn x() {\n}\n"})

	if _, _, err := LoadDirAs(root, "app"); err == nil || !strings.Contains(err.Error(), "bad-dir is not a name") {
		t.Errorf("%v", err)
	}
}

func TestAFileUnderAnotherName(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"my-script.cov": "fn f() {\n}\n"})

	file := filepath.Join(dir, "my-script.cov")

	if _, err := LoadFiles([]string{file}); err == nil {
		t.Error("a file with a name that is not a module name")
	}

	mods, err := LoadFiles([]string{"script=" + file})
	if err != nil || len(mods["script"]) != 1 || mods["script"][0].Name != file {
		t.Errorf("%v %v", mods, err)
	}

	if _, err := LoadFiles([]string{"bad-name=" + file}); err == nil || !strings.Contains(err.Error(), "bad-name is not a name") {
		t.Errorf("%v", err)
	}
}

// A path that is there is that path, even if it could be read as a name and a path: x=y is the directory x=y, and not the name x
// for the directory y, when both are there.
func TestAPathWithEqualsInItIsThePath(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"x=y/a.cov": "fn a() {\n}\n", "y/b.cov": "fn b() {\n}\n"})

	t.Chdir(dir)

	if name, path := SplitMount("x=y"); name != "" || path != "x=y" {
		t.Errorf("(%q, %q)", name, path)
	}

	// and when only y is there, it is the name x for it
	if err := os.RemoveAll(filepath.Join(dir, "x=y")); err != nil {
		t.Fatal(err)
	}

	if name, path := SplitMount("x=y"); name != "x" || path != "y" {
		t.Errorf("(%q, %q)", name, path)
	}
}
