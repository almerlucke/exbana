// Package gentest compiles and runs generated code, for the tests of the code generator: the files are written to a module
// of their own in a temporary directory, which is built and tested with the go tool.
package gentest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Run writes the files (by name) to a new module and runs its tests. It fails the test, with the output, if they fail.
// The module has no dependencies, which is the point: generated parsers do not use this library.
func Run(t testing.TB, files map[string]string) string {
	t.Helper()

	return RunWith(t, files)
}

// RunWith is Run with arguments for go test, like "-bench", ".".
func RunWith(t testing.TB, files map[string]string, args ...string) string {
	t.Helper()

	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not available")
	}

	dir := t.TempDir()

	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module probe\n\ngo 1.24\n"
	}

	for name, content := range files {
		path := filepath.Join(dir, name)

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, goTool, append([]string{"test", "-count=1"}, append(args, "./...")...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GO111MODULE=on")

	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out

	if err := cmd.Run(); err != nil {
		t.Fatalf("the generated code does not build or its tests fail: %v\n%s", err, tail(out.String(), 6000))
	}

	return out.String()
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return "...\n" + s[len(s)-n:]
}
