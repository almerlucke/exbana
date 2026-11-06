// Package std is the standard library for Covenant programs that do things in the world: the modules a host gives them, and the
// policy that says how far each of them goes.
//
// A program can only do what its signatures say (the checker proves it), and only what the host grants: an effect is granted when
// the policy has something for it. The effects are the ones in the names of the modules:
//
//	io::print   io::read   fs::read   fs::write   net::fetch   env::read   process::run   time   random
//
// and the policy is the other half of each. Reading a file needs fs::read granted and the file to be under one of ReadRoots;
// a request needs net::fetch and a host that is on the list; a program runs only if it is on the list. Nothing is allowed that
// the policy does not name, so the zero Policy lets a program print and nothing else.
package std

import (
	"bufio"
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/almerlucke/exbana/v3/examples/covenant/run"
)

// Policy is what programs are allowed to do. A list that is empty allows nothing, and "*" in a list allows everything.
type Policy struct {
	ReadRoots  []string // directories that fs can read from
	WriteRoots []string // directories that fs can write to
	NetHosts   []string // hosts that net can talk to: "example.com", "example.com:8080", "*.example.com"
	EnvVars    []string // names of the environment variables that env can read
	Programs   []string // programs that process can run, by the name the program gives

	AllowStdin  bool // io::read
	AllowTime   bool // time
	AllowRandom bool // random

	// AllowPrivateNet lets net reach loopback and private addresses, which it does not do otherwise: a program that can be sent
	// to any address can be made to talk to what is inside the network.
	AllowPrivateNet bool

	Timeout time.Duration // for a request and for a process; 30 seconds if it is zero
	MaxData int64         // the most that is read from a file, a response or a process; 10 MiB if it is zero

	// What the programs get to see of the world: the defaults are the real ones.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Now            func() time.Time
	Rand           *rand.Rand
	Client         *http.Client // for a request: if it is not set, one is made that keeps to the policy
}

// Granted are the effects the policy grants. io::print is always granted.
func (p Policy) Granted() []string {
	out := []string{"io::print"}

	add := func(ok bool, name string) {
		if ok {
			out = append(out, name)
		}
	}

	add(len(p.ReadRoots) > 0, "fs::read")
	add(len(p.WriteRoots) > 0, "fs::write")
	add(len(p.NetHosts) > 0, "net::fetch")
	add(len(p.EnvVars) > 0, "env::read")
	add(len(p.Programs) > 0, "process::run")
	add(p.AllowStdin, "io::read")
	add(p.AllowTime, "time")
	add(p.AllowRandom, "random")

	sort.Strings(out)

	return out
}

func (p Policy) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}

	return 30 * time.Second
}

func (p Policy) maxData() int64 {
	if p.MaxData > 0 {
		return p.MaxData
	}

	return 10 << 20
}

// Std is the host side of the modules, for one program.
type Std struct {
	policy Policy
	prog   *run.Program

	mu     sync.Mutex
	stdin  *bufio.Reader
	client *http.Client
}

// New makes the host side of the modules for a policy.
func New(p Policy) *Std {
	if p.Stdin == nil {
		p.Stdin = os.Stdin
	}

	if p.Stdout == nil {
		p.Stdout = os.Stdout
	}

	if p.Stderr == nil {
		p.Stderr = os.Stderr
	}

	if p.Now == nil {
		p.Now = time.Now
	}

	return &Std{policy: p}
}

// Bind says which program the host functions are for: they make values of its types.
func (s *Std) Bind(prog *run.Program) { s.prog = prog }

// Policy is the policy the host was made with.
func (s *Std) Policy() Policy { return s.policy }

// Natives are the modules, as the source the checker reads for a module that a host provides.
func Natives() map[string]string {
	return map[string]string{
		"io": ioSource, "fs": fsSource, "net": netSource, "env": envSource, "process": processSource, "clock": clockSource,
		"random": randomSource,
	}
}

const ioSource = `
trait Functions {
    fn print(text: Text) with io::print
    fn eprint(text: Text) with io::print
    fn read_line() with io::read -> Option<Text>
    fn read_all() with io::read -> Text
}
`

const clockSource = `
trait Functions {
    fn now() with time -> Int
    fn sleep(millis: Int) with time
}
`

const randomSource = `
trait Functions {
    fn int(low: Int, high: Int) with random -> Int
    fn float() with random -> Float
}
`

// Host is the functions of the modules, to give to run.Config.
func (s *Std) Host() map[string]run.HostFunc {
	h := map[string]run.HostFunc{}

	for _, add := range []func(map[string]run.HostFunc){s.ioHost, s.fsHost, s.netHost, s.envHost, s.processHost, s.clockHost, s.randomHost} {
		add(h)
	}

	return h
}

func (s *Std) ioHost(h map[string]run.HostFunc) {
	write := func(w io.Writer) run.HostFunc {
		return func(ctx context.Context, a []run.Value) (run.Value, error) {
			_, err := io.WriteString(w, a[0].(string)+"\n")
			return run.Unit{}, err
		}
	}

	h["io::print"] = write(s.policy.Stdout)
	h["io::eprint"] = write(s.policy.Stderr)

	reader := func() *bufio.Reader {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.stdin == nil {
			s.stdin = bufio.NewReader(s.policy.Stdin)
		}

		return s.stdin
	}

	h["io::read_line"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		line, err := reader().ReadString('\n')
		if err != nil && line == "" {
			return s.prog.None(), nil
		}

		line = trimEOL(line)

		return s.prog.Some(line), nil
	}

	h["io::read_all"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		data, err := io.ReadAll(io.LimitReader(reader(), s.policy.maxData()))
		return string(data), err
	}
}

func trimEOL(s string) string {
	if n := len(s); n > 0 && s[n-1] == '\n' {
		s = s[:n-1]
		if n := len(s); n > 0 && s[n-1] == '\r' {
			s = s[:n-1]
		}
	}

	return s
}

func (s *Std) clockHost(h map[string]run.HostFunc) {
	h["clock::now"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		return s.policy.Now().UnixMilli(), nil
	}

	h["clock::sleep"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		t := time.NewTimer(time.Duration(a[0].(int64)) * time.Millisecond)
		defer t.Stop()

		select {
		case <-t.C:
			return run.Unit{}, nil
		case <-ctx.Done():
			return nil, &run.Limit{Kind: "cancelled", Err: ctx.Err()}
		}
	}
}

func (s *Std) randomHost(h map[string]run.HostFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r := s.policy.Rand
	if r == nil {
		r = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 1))
	}

	h["random::int"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		low, high := a[0].(int64), a[1].(int64)
		if high <= low {
			return nil, errEmptyRange
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		return low + r.Int64N(high-low), nil
	}

	h["random::float"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		s.mu.Lock()
		defer s.mu.Unlock()

		return r.Float64(), nil
	}
}
