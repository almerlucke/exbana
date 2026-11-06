package check

import (
	"strings"
	"testing"
)

func checkModules(t *testing.T, mods map[string]string) *Result {
	t.Helper()

	cfg := testConfig
	cfg.Effects = append(append([]string(nil), cfg.Effects...), "canvas::draw")

	return Check(cfg, mods)
}

const geometry = `
pub struct Point { x: Float, y: Float }

pub enum Kind {
    Round
    Square
}

pub effects Drawing = canvas::draw + time

pub fn origin() -> Point {
    return Point(x: 0.0, y: 0.0)
}

pub fn paint() with Drawing -> Int {
    return 1
}

fn helper() -> Int {
    return 1
}

impl Point {
    pub fn norm(self) -> Float {
        return self.x + self.y
    }

    fn secret(self) -> Float {
        return self.x
    }
}
`

func TestModules(t *testing.T) {
	r := checkModules(t, map[string]string{
		"geometry": geometry,
		"app": `
use geometry
use geometry::Point
use geometry::Drawing
use geometry::origin as zero

fn f() -> Float {
    let p = geometry::origin()
    let q: Point = zero()
    let k = geometry::Kind::Round
    return p.x + q.norm()
}

fn g() with Drawing -> Int {
    return geometry::paint()
}

fn h(k: geometry::Kind) -> Int {
    return match k {
        geometry::Kind::Round => 1
        geometry::Kind::Square => 2
    }
}
`,
	})

	if !r.OK() {
		t.Fatal(r.Error())
	}
}

func TestModuleProblems(t *testing.T) {
	for _, c := range []struct {
		name, app string
		mods      map[string]string
		module    string
		line      int
		code, msg string
	}{
		{"a private function", "use geometry\nfn f() -> Int {\n    return geometry::helper()\n}\n", nil, "app", 3, "private", "helper is not public"},
		{"a private function in use", "use geometry::helper\nfn f() {\n}\n", nil, "app", 1, "private", "helper is not public"},
		{"a private method", "use geometry\nfn f(p: geometry::Point) -> Float {\n    return p.secret()\n}\n", nil, "app", 3, "private", "the method secret is not public"},
		{"an unknown module", "use nope\nfn f() {\n}\n", nil, "app", 1, "unknown-module", "there is no module nope"},
		{"an unknown item", "use geometry::Nope\nfn f() {\n}\n", nil, "app", 1, "unknown-name", "module geometry has no Nope"},
		{"a module that is not used", "fn f() -> Int {\n    return geometry::origin().x\n}\n", nil, "app", 2, "unknown-name", "there is no geometry::origin"},
		{"effects of another module", "use geometry\nfn f() -> Int {\n    return geometry::paint()\n}\n", nil, "app", 3, "effect", "this needs the effect canvas::draw + time"},
		{"a name declared and used", "use geometry::Point\nstruct Point { x: Int }\n", nil, "app", 1, "duplicate", "Point is already declared"},
		{"modules that use each other", "use b\nfn f() {\n}\n", map[string]string{"b": "use app\nfn g() {\n}\n"}, "", 0, "cycle", "modules use each other"},
		{"a syntax error in a module", "fn f() {\n}\n", map[string]string{"geometry": "fn f( {\n"}, "geometry", 1, "syntax", "unexpected"},
	} {
		t.Run(c.name, func(t *testing.T) {
			mods := map[string]string{"geometry": geometry, "app": c.app}
			for k, v := range c.mods {
				mods[k] = v
			}

			r := checkModules(t, mods)

			if r.OK() {
				t.Fatalf("no problem found in\n%s", c.app)
			}

			for _, d := range r.Diagnostics {
				if (c.module == "" || d.Module == c.module) && (c.line == 0 || d.Line == c.line) && d.Code == c.code && strings.Contains(d.Msg, c.msg) {
					return
				}
			}

			t.Fatalf("want %s in %s at line %d with %q, got:\n%v", c.code, c.module, c.line, c.msg, r.Error())
		})
	}
}

func TestNativeModulesDeclareEffects(t *testing.T) {
	// fs::read is not in the configuration: the module that provides fs says it is an effect
	r := Check(Config{Native: testConfig.Native}, map[string]string{"app": "use fs\nuse fs::Path\nfn f(p: Path) with fs::read {\n    let r = fs::read_all(p)\n}\n"})

	if !r.OK() {
		t.Fatal(r.Error())
	}

	// and an effect nobody provides is not one
	r = Check(Config{}, map[string]string{"app": "fn f() with fs::read {\n}\n"})
	if r.OK() {
		t.Fatal("an effect nobody knows was accepted")
	}
}

func TestAModuleCanNotTakeTheNameOfOneOfTheHost(t *testing.T) {
	r := Check(Config{Native: testConfig.Native}, map[string]string{"app": "fn f() {\n}\n", "fs": "pub fn read() -> Int {\n    return 1\n}\n"})

	if r.OK() || len(r.Diagnostics) != 1 || r.Diagnostics[0].Module != "fs" || !strings.Contains(r.Diagnostics[0].Msg, "one the host provides") {
		t.Errorf("%v", r.Error())
	}
}
