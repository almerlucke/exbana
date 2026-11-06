package std_test

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
	"github.com/almerlucke/exbana/v3/examples/covenant/run"
	"github.com/almerlucke/exbana/v3/examples/covenant/std"
)

// build checks a program that uses the modules, and makes it a program that runs with the policy.
func build(t *testing.T, p std.Policy, src string, bytecode bool) *run.Program {
	t.Helper()

	res := check.Check(check.Config{Native: std.Natives()}, map[string]string{"app": src})
	if !res.OK() {
		t.Fatalf("%v\n%s", res.Error(), src)
	}

	s := std.New(p)

	prog, err := run.New(res, run.Config{Host: s.Host(), Grant: p.Granted(), Bytecode: bytecode})
	if err != nil {
		t.Fatal(err)
	}

	s.Bind(prog)

	return prog
}

// call runs a function both ways, which have to give the same, and says what it gave.
func call(t *testing.T, p std.Policy, src, fn string, args ...run.Value) (string, error) {
	t.Helper()

	var got [2]string

	var errs [2]error

	for i, bc := range []bool{false, true} {
		prog := build(t, p, src, bc)

		v, err := prog.Call(context.Background(), "app", fn, args...)
		if err == nil {
			got[i] = run.Format(v)
		}

		errs[i] = err
	}

	if got[0] != got[1] || (errs[0] == nil) != (errs[1] == nil) {
		t.Fatalf("the tree and the bytecode differ: %q (%v) and %q (%v)", got[0], errs[0], got[1], errs[1])
	}

	return got[0], errs[0]
}

func must(t *testing.T, p std.Policy, src, fn string, args ...run.Value) string {
	t.Helper()

	got, err := call(t, p, src, fn, args...)
	if err != nil {
		t.Fatalf("%s: %v", fn, err)
	}

	return got
}

func TestGranted(t *testing.T) {
	if got := strings.Join(std.Policy{}.Granted(), " "); got != "io::print" {
		t.Errorf("nothing is allowed, and %s is granted", got)
	}

	all := std.Policy{
		ReadRoots: []string{"."}, WriteRoots: []string{"."}, NetHosts: []string{"x"}, EnvVars: []string{"X"}, Programs: []string{"x"},
		AllowStdin: true, AllowTime: true, AllowRandom: true,
	}

	want := "env::read fs::read fs::write io::print io::read net::fetch process::run random time"
	if got := strings.Join(all.Granted(), " "); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

const fsProgram = `
use fs

fn roundtrip(path: Text) with fs::read + fs::write -> Result<Text, fs::IoError> {
    try fs::write(path, "hello")
    try fs::append(path, " world")
    return fs::read(path)
}

fn kind(path: Text) with fs::read -> Text {
    return match fs::read(path) {
        Ok(t) => "ok " + t
        Err(e) => match e {
            NotFound => "notfound"
            Denied => "denied"
            Failed(m) => "failed"
        }
    }
}

fn names(path: Text) with fs::read -> List<Text> {
    return fs::list(path).unwrap_or([])
}

fn exists(path: Text) with fs::read -> Result<Bool, fs::IoError> {
    return fs::exists(path)
}

fn tidy(path: Text) with fs::write -> Result<Unit, fs::IoError> {
    try fs::make_dir(path + "/sub/dir")
    return fs::remove(path + "/sub/dir")
}
`

func TestFiles(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")

	for _, d := range []string{dir, outside, filepath.Join(dir, "inner")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	p := std.Policy{ReadRoots: []string{dir}, WriteRoots: []string{dir}}

	if got := must(t, p, fsProgram, "roundtrip", filepath.Join(dir, "a.txt")); got != `Ok("hello world")` {
		t.Errorf("roundtrip: %s", got)
	}

	for path, want := range map[string]string{
		filepath.Join(dir, "a.txt"):                       `"ok hello world"`,
		filepath.Join(dir, "none.txt"):                    `"notfound"`,
		filepath.Join(outside, "secret.txt"):              `"denied"`,
		filepath.Join(dir, "..", "outside", "secret.txt"): `"denied"`,
		filepath.Join(dir, "link", "secret.txt"):          `"denied"`, // a link that goes out does not lead out
		"/etc/hostname":                                   `"denied"`,
		dir + "/inner/../a.txt":                           `"ok hello world"`,
	} {
		if got := must(t, p, fsProgram, "kind", path); got != want {
			t.Errorf("reading %s: %s, want %s", path, got, want)
		}
	}

	if got := must(t, p, fsProgram, "names", dir); got != `["a.txt", "inner/", "link"]` {
		t.Errorf("names: %s", got)
	}

	if got := must(t, p, fsProgram, "exists", filepath.Join(dir, "a.txt")); got != "Ok(true)" {
		t.Errorf("exists: %s", got)
	}

	if got := must(t, p, fsProgram, "exists", filepath.Join(dir, "nope")); got != "Ok(false)" {
		t.Errorf("does not exist: %s", got)
	}

	if got := must(t, p, fsProgram, "exists", outside); got != "Err(IoError::Denied)" {
		t.Errorf("exists outside: %s", got)
	}

	if got := must(t, p, fsProgram, "tidy", dir); got != "Ok(Unit)" {
		t.Errorf("tidy: %s", got)
	}

	if _, err := os.Stat(filepath.Join(dir, "sub")); err != nil {
		t.Errorf("make_dir: %v", err)
	}

	// a link that goes out can not be written through either
	if got := must(t, p, fsProgram, "roundtrip", filepath.Join(dir, "link", "new.txt")); got != "Err(IoError::Denied)" {
		t.Errorf("writing through a link: %s", got)
	}

	if _, err := os.Stat(filepath.Join(outside, "new.txt")); err == nil {
		t.Error("a file was written outside the root")
	}
}

func TestReadingIsNotWriting(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()

	// the effects have to be granted: only reading is, so a function that writes is not called
	_, err := call(t, std.Policy{ReadRoots: []string{dir}}, fsProgram, "roundtrip", filepath.Join(dir, "a.txt"))

	var d *run.Denied
	if !errors.As(err, &d) || len(d.Effects) != 1 || d.Effects[0] != "fs::write" {
		t.Errorf("%v", err)
	}

	// and where they are granted, the place has to be one that is allowed
	p := std.Policy{ReadRoots: []string{dir}, WriteRoots: []string{other}}

	if got := must(t, p, fsProgram, "roundtrip", filepath.Join(dir, "a.txt")); got != "Err(IoError::Denied)" {
		t.Errorf("writing where only reading is allowed: %s", got)
	}

	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err == nil {
		t.Error("a file was written where only reading is allowed")
	}
}

func TestEveryPlaceWhenAskedFor(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x.txt")

	if got := must(t, std.Policy{ReadRoots: []string{"*"}, WriteRoots: []string{"*"}}, fsProgram, "roundtrip", file); got != `Ok("hello world")` {
		t.Errorf("%s", got)
	}
}

const netProgram = `
use net

fn fetch(url: Text) with net::fetch -> Text {
    return match net::get(url) {
        Ok(r) => r.status.to_text() + ":" + r.body + ":" + r.headers.get("x-test").unwrap_or("-")
        Err(e) => match e {
            Denied => "denied"
            Failed(m) => "failed"
        }
    }
}

fn send(url: Text) with net::fetch -> Text {
    return match net::post(url, "payload", {"x-token": "abc"}) {
        Ok(r) => r.body
        Err(e) => match e {
            Denied => "denied"
            Failed(m) => "failed"
        }
    }
}
`

func TestNetwork(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("other")) }))
	defer other.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hello":
			w.Header().Set("X-Test", "yes")
			w.WriteHeader(201)
			_, _ = w.Write([]byte("hi"))
		case "/post":
			body := new(bytes.Buffer)
			_, _ = body.ReadFrom(r.Body)
			_, _ = w.Write([]byte(r.Method + " " + body.String() + " " + r.Header.Get("X-Token")))
		case "/redirect":
			http.Redirect(w, r, other.URL+"/x", http.StatusFound)
		case "/big":
			_, _ = w.Write(bytes.Repeat([]byte("x"), 5000))
		case "/slow":
			time.Sleep(time.Second)
		}
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "http://")
	justHost, _, _ := strings.Cut(host, ":")
	p := std.Policy{NetHosts: []string{justHost}, AllowPrivateNet: true}

	if got := must(t, p, netProgram, "fetch", server.URL+"/hello"); got != `"201:hi:yes"` {
		t.Errorf("get: %s", got)
	}

	if got := must(t, p, netProgram, "send", server.URL+"/post"); got != `"POST payload abc"` {
		t.Errorf("post: %s", got)
	}

	// a host that is not on the list, a scheme that is not http
	for url, want := range map[string]string{
		"http://example.org/":  `"denied"`,
		"ftp://" + host + "/x": `"denied"`,
		"file:///etc/passwd":   `"denied"`,
		"not a url":            `"denied"`,
	} {
		if got := must(t, p, netProgram, "fetch", url); got != want {
			t.Errorf("%s: %s, want %s", url, got, want)
		}
	}

	// a redirect to a host that is not on the list is not followed: the list is this server and its port, and the other server is
	// another port
	onlyThis := std.Policy{NetHosts: []string{host}, AllowPrivateNet: true}
	if got := must(t, onlyThis, netProgram, "fetch", server.URL+"/redirect"); got != `"denied"` {
		t.Errorf("a redirect to another host: %s", got)
	}

	// and when the other is on the list too it is followed
	both := std.Policy{NetHosts: []string{host, strings.TrimPrefix(other.URL, "http://")}, AllowPrivateNet: true}
	if got := must(t, both, netProgram, "fetch", server.URL+"/redirect"); got != `"200:other:-"` {
		t.Errorf("a redirect to a host that is allowed: %s", got)
	}

	// the same host by its port only, and by a pattern
	for _, list := range [][]string{{host}, {"*"}, {"*." + justHost}} {
		got := must(t, std.Policy{NetHosts: list, AllowPrivateNet: true}, netProgram, "fetch", server.URL+"/hello")
		if want := `"201:hi:yes"`; got != want && list[0] != "*."+justHost {
			t.Errorf("hosts %v: %s", list, got)
		}
	}

	// the loopback is not reached unless it is said that it may be
	p.AllowPrivateNet = false
	if got := must(t, p, netProgram, "fetch", server.URL+"/hello"); got != `"failed"` {
		t.Errorf("a private address: %s", got)
	}

	// no more is read than the most, and no longer is waited than the time
	p = std.Policy{NetHosts: []string{justHost}, AllowPrivateNet: true, MaxData: 1000}
	if got := must(t, p, netProgram, "fetch", server.URL+"/big"); got != `"failed"` {
		t.Errorf("a response that is too large: %s", got)
	}

	p = std.Policy{NetHosts: []string{justHost}, AllowPrivateNet: true, Timeout: 100 * time.Millisecond}
	if got := must(t, p, netProgram, "fetch", server.URL+"/slow"); got != `"failed"` {
		t.Errorf("a response that is too slow: %s", got)
	}
}

const envProgram = `
use env

fn get(name: Text) with env::read -> Text {
    return match env::get(name) {
        Ok(v) => v
        Err(e) => match e {
            NotSet => "not set"
            Denied => "denied"
        }
    }
}
`

func TestEnvironment(t *testing.T) {
	t.Setenv("COVENANT_TEST_VISIBLE", "visible")
	t.Setenv("COVENANT_TEST_HIDDEN", "hidden")

	p := std.Policy{EnvVars: []string{"COVENANT_TEST_VISIBLE", "COVENANT_TEST_UNSET"}}

	for name, want := range map[string]string{
		"COVENANT_TEST_VISIBLE": `"visible"`, "COVENANT_TEST_UNSET": `"not set"`, "COVENANT_TEST_HIDDEN": `"denied"`, "HOME": `"denied"`,
	} {
		if got := must(t, p, envProgram, "get", name); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}

	if got := must(t, std.Policy{EnvVars: []string{"*"}}, envProgram, "get", "COVENANT_TEST_HIDDEN"); got != `"hidden"` {
		t.Errorf("any: %s", got)
	}
}

const processProgram = `
use process

fn run(program: Text, args: List<Text>) with process::run -> Text {
    return match process::run(program, args) {
        Ok(o) => o.code.to_text() + "|" + o.stdout.trim() + "|" + o.stderr.trim()
        Err(e) => match e {
            Denied => "denied"
            Failed(m) => "failed"
        }
    }
}
`

func TestProcesses(t *testing.T) {
	words := func(w ...string) run.Value {
		items := make([]run.Value, len(w))
		for i, x := range w {
			items[i] = x
		}

		return run.NewList(items)
	}

	p := std.Policy{Programs: []string{"echo", "sh"}}

	for _, c := range []struct {
		program string
		args    run.Value
		want    string
	}{
		{"echo", words("hello", "world"), `"0|hello world|"`},
		{"sh", words("-c", "echo out; echo err >&2; exit 3"), `"3|out|err"`},
		{"sh", words("-c", "echo $HOME-$SECRET"), `"0|-|"`}, // the program has no environment of ours
		{"cat", words("/etc/passwd"), `"denied"`},
		{"rm", words("-rf", "/"), `"denied"`},
		{"nonexistent-program-xyz", words(), `"denied"`},
	} {
		t.Setenv("SECRET", "leaked")

		if got := must(t, p, processProgram, "run", c.program, c.args); got != c.want {
			t.Errorf("%s %v: %s, want %s", c.program, c.args, got, c.want)
		}
	}

	// a program that is allowed and is not there
	if got := must(t, std.Policy{Programs: []string{"nonexistent-program-xyz"}}, processProgram, "run", "nonexistent-program-xyz", words()); got != `"failed"` {
		t.Errorf("a program that is not there: %s", got)
	}

	slow := std.Policy{Programs: []string{"sleep"}, Timeout: 100 * time.Millisecond}

	start := time.Now()
	if got := must(t, slow, processProgram, "run", "sleep", words("5")); got != `"failed"` || time.Since(start) > 3*time.Second {
		t.Errorf("a program that runs too long: %s after %v", got, time.Since(start))
	}

	chatty := std.Policy{Programs: []string{"sh"}, MaxData: 100}
	if got := must(t, chatty, processProgram, "run", "sh", words("-c", "yes | head -c 10000")); got != `"failed"` {
		t.Errorf("a program that writes too much: %.80s", got)
	}

	if got := must(t, std.Policy{Programs: []string{"*"}}, processProgram, "run", "echo", words("any")); got != `"0|any|"` {
		t.Errorf("any: %s", got)
	}
}

func TestInputAndOutput(t *testing.T) {
	src := `
use io

fn echo() with io::read + io::print {
    match io::read_line() {
        Some(line) => io::print("got " + line)
        None => io::print("eof")
    }
}

fn rest() with io::read + io::print {
    io::print("[" + io::read_all() + "]")
    io::eprint("done")
}
`

	for _, bc := range []bool{false, true} {
		var out, errs bytes.Buffer

		p := std.Policy{AllowStdin: true, Stdin: strings.NewReader("first\r\nsecond\nthird"), Stdout: &out, Stderr: &errs}
		prog := build(t, p, src, bc)

		for range 2 {
			if _, err := prog.Call(context.Background(), "app", "echo"); err != nil {
				t.Fatal(err)
			}
		}

		if _, err := prog.Call(context.Background(), "app", "rest"); err != nil {
			t.Fatal(err)
		}

		if out.String() != "got first\ngot second\n[third]\n" || errs.String() != "done\n" {
			t.Errorf("bytecode=%v: %q and %q", bc, out.String(), errs.String())
		}

		// at the end there is no more
		if _, err := prog.Call(context.Background(), "app", "echo"); err != nil || !strings.HasSuffix(out.String(), "eof\n") {
			t.Errorf("at the end: %q, %v", out.String(), err)
		}
	}

	// reading is not granted unless it is allowed
	_, err := call(t, std.Policy{}, src, "echo")

	var d *run.Denied
	if !errors.As(err, &d) || d.Effects[0] != "io::read" {
		t.Errorf("%v", err)
	}
}

func TestClockAndRandom(t *testing.T) {
	src := `
use clock
use random

fn now() with time -> Int {
    return clock::now()
}

fn pause(ms: Int) with time {
    clock::sleep(ms)
}

fn dice() with random -> List<Int> {
    return [random::int(1, 7), random::int(1, 7), random::int(1, 7)]
}

fn unit() with random -> Float {
    return random::float()
}

fn bad() with random -> Int {
    return random::int(5, 5)
}
`

	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	p := std.Policy{AllowTime: true, AllowRandom: true, Now: func() time.Time { return fixed }}

	if got := must(t, p, src, "now"); got != "1767323045000" {
		t.Errorf("now: %s", got)
	}

	// the same seed gives the same dice, which is what makes a program that has random testable
	seeded := func() std.Policy {
		q := p
		q.Rand = rand.New(rand.NewPCG(1, 2))

		return q
	}

	// each way of running gets a generator of its own: they would take turns with one
	dice := func() string {
		var got [2]string

		for i, bc := range []bool{false, true} {
			v, err := build(t, seeded(), src, bc).Call(context.Background(), "app", "dice")
			if err != nil {
				t.Fatal(err)
			}

			got[i] = run.Format(v)
		}

		if got[0] != got[1] {
			t.Fatalf("the tree and the bytecode differ: %s and %s", got[0], got[1])
		}

		return got[0]
	}

	first := dice()
	if again := dice(); first != again {
		t.Errorf("%s and %s", first, again)
	}

	for _, line := range strings.Split(strings.Trim(first, "[]"), ", ") {
		if line < "1" || line > "6" || len(line) != 1 {
			t.Errorf("a die of %s in %s", line, first)
		}
	}

	v, err := build(t, seeded(), src, true).Call(context.Background(), "app", "unit")
	if f, ok := v.(float64); err != nil || !ok || f < 0 || f >= 1 {
		t.Errorf("float: %v, %v", v, err)
	}

	if _, err := build(t, seeded(), src, true).Call(context.Background(), "app", "bad"); err == nil || !strings.Contains(err.Error(), "low < high") {
		t.Errorf("an empty range: %v", err)
	}

	// time and random are not granted unless they are allowed
	_, err = call(t, std.Policy{}, src, "now")

	var d *run.Denied
	if !errors.As(err, &d) || d.Effects[0] != "time" {
		t.Errorf("%v", err)
	}

	// a sleep ends when the call does
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	prog := build(t, p, src, true)

	_, err = prog.Call(ctx, "app", "pause", int64(5000))

	var l *run.Limit
	if !errors.As(err, &l) || l.Kind != "cancelled" {
		t.Errorf("a sleep that is cancelled: %v", err)
	}
}

// A root can be a link to a directory (a temporary directory often is), and what is inside it is inside.
func TestARootThatIsALink(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")

	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(real, "a.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	p := std.Policy{ReadRoots: []string{link}}

	for _, path := range []string{filepath.Join(link, "a.txt"), filepath.Join(real, "a.txt")} {
		if got := must(t, p, fsProgram, "kind", path); got != `"ok inside"` {
			t.Errorf("%s: %s", path, got)
		}
	}
}
