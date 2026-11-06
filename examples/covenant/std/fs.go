package std

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/run"
)

var errEmptyRange = errors.New("random::int needs low < high")

const fsSource = `
enum IoError {
    NotFound
    Denied
    Failed(message: Text)
}

trait Functions {
    fn read(path: Text) with fs::read -> Result<Text, IoError>
    fn exists(path: Text) with fs::read -> Result<Bool, IoError>
    fn list(path: Text) with fs::read -> Result<List<Text>, IoError>
    fn write(path: Text, data: Text) with fs::write -> Result<Unit, IoError>
    fn append(path: Text, data: Text) with fs::write -> Result<Unit, IoError>
    fn remove(path: Text) with fs::write -> Result<Unit, IoError>
    fn make_dir(path: Text) with fs::write -> Result<Unit, IoError>
}
`

// resolve is the real place of a path, if it is inside one of the roots. Links are followed before the check, so a link inside a
// root that goes outside it does not lead out; a path that is not there yet is looked at from the part of it that is.
func resolve(path string, roots []string) (string, bool) {
	if len(roots) == 0 {
		return "", false
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}

	real, err := evalExisting(abs)
	if err != nil {
		return "", false
	}

	for _, root := range roots {
		if root == "*" {
			return real, true
		}

		rabs, err := filepath.Abs(root)
		if err != nil {
			continue
		}

		rreal, err := evalExisting(rabs)
		if err != nil {
			continue
		}

		if inside(rreal, real) {
			return real, true
		}
	}

	return "", false
}

// evalExisting follows the links of the part of a path that exists, and keeps the rest as it is.
func evalExisting(path string) (string, error) {
	rest := ""

	for p := path; ; p = filepath.Dir(p) {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(real, rest), nil
		}

		if filepath.Dir(p) == p {
			return "", fmt.Errorf("no part of %s exists", path)
		}

		rest = filepath.Join(filepath.Base(p), rest)
	}
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)

	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Std) ioError(err error) run.Value {
	kind, msg := "Failed", err.Error()

	switch {
	case errors.Is(err, fs.ErrNotExist):
		kind = "NotFound"
	case errors.Is(err, fs.ErrPermission):
		kind = "Denied"
	}

	if kind == "Failed" {
		return s.fail("fs", "IoError", kind, msg)
	}

	return s.fail("fs", "IoError", kind)
}

// fail is an Err of an enum of a module.
func (s *Std) fail(module, enum, variant string, fields ...run.Value) run.Value {
	v, err := s.prog.Variant(module, enum, variant, fields...)
	if err != nil {
		panic(err)
	}

	return s.prog.Err(v)
}

func (s *Std) denied(module, enum string) run.Value { return s.fail(module, enum, "Denied") }

func (s *Std) fsHost(h map[string]run.HostFunc) {
	// readPath and writePath are what a function does with a path that is allowed, or the Err for one that is not
	with := func(roots func() []string, do func(path string, a []run.Value) (run.Value, error)) run.HostFunc {
		return func(ctx context.Context, a []run.Value) (run.Value, error) {
			real, ok := resolve(a[0].(string), roots())
			if !ok {
				return s.denied("fs", "IoError"), nil
			}

			v, err := do(real, a)
			if err != nil {
				return s.ioError(err), nil
			}

			return s.prog.Ok(v), nil
		}
	}

	read := func() []string { return s.policy.ReadRoots }
	write := func() []string { return s.policy.WriteRoots }

	h["fs::read"] = with(read, func(path string, a []run.Value) (run.Value, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}

		defer f.Close()

		data, err := io.ReadAll(io.LimitReader(f, s.policy.maxData()+1))
		if err != nil {
			return nil, err
		}

		if int64(len(data)) > s.policy.maxData() {
			return nil, errors.New("the file is larger than the most that is read")
		}

		return string(data), nil
	})

	h["fs::exists"] = with(read, func(path string, a []run.Value) (run.Value, error) {
		_, err := os.Stat(path)

		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, fs.ErrNotExist):
			return false, nil
		}

		return nil, err
	})

	h["fs::list"] = with(read, func(path string, a []run.Value) (run.Value, error) {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}

		names := make([]string, len(entries))

		for i, e := range entries {
			names[i] = e.Name()
			if e.IsDir() {
				names[i] += "/"
			}
		}

		sort.Strings(names)

		items := make([]run.Value, len(names))
		for i, n := range names {
			items[i] = n
		}

		return run.NewList(items), nil
	})

	h["fs::write"] = with(write, func(path string, a []run.Value) (run.Value, error) {
		return run.Unit{}, os.WriteFile(path, []byte(a[1].(string)), 0o644)
	})

	h["fs::append"] = with(write, func(path string, a []run.Value) (run.Value, error) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}

		_, err = f.WriteString(a[1].(string))

		return run.Unit{}, errors.Join(err, f.Close())
	})

	h["fs::remove"] = with(write, func(path string, a []run.Value) (run.Value, error) {
		return run.Unit{}, os.Remove(path)
	})

	h["fs::make_dir"] = with(write, func(path string, a []run.Value) (run.Value, error) {
		return run.Unit{}, os.MkdirAll(path, 0o755)
	})
}
