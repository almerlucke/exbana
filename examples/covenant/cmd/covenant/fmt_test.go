package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	messy = "fn  main( ) {\n  io::print( \"hi\" )   // say hi\n}\n"
	neat  = "fn main() {\n    io::print(\"hi\") // say hi\n}\n"
)

func write(t *testing.T, path, text string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func TestFmtWritesTheSource(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.cov")
	write(t, file, messy)

	code, out, errs := cmd("fmt", file)
	if code != 0 || out != neat || errs != "" {
		t.Errorf("fmt: %d %q %q", code, out, errs)
	}

	if read(t, file) != messy {
		t.Error("without -w the file is as it was")
	}
}

func TestFmtFromTheStandardInput(t *testing.T) {
	defer func(r io.Reader) { stdin = r }(stdin)

	stdin = strings.NewReader(messy)

	code, out, errs := cmd("fmt", "-")
	if code != 0 || out != neat || errs != "" {
		t.Errorf("fmt -: %d %q %q", code, out, errs)
	}

	// an editor asks for the source of a buffer that is not Covenant (yet): it is an error, and nothing is written
	stdin = strings.NewReader("fn (")

	code, out, errs = cmd("fmt", "-")
	if code != 1 || out != "" || !strings.Contains(errs, "<stdin>: 1:") {
		t.Errorf("fmt - with a mistake: %d %q %q", code, out, errs)
	}
}

func TestFmtRewritesWhatIsNotFormatted(t *testing.T) {
	dir := t.TempDir()
	messyFile, neatFile := filepath.Join(dir, "sub", "messy.cov"), filepath.Join(dir, "neat.cov")
	skipped, other := filepath.Join(dir, "_vendor", "skip.cov"), filepath.Join(dir, "notes.txt")

	write(t, messyFile, messy)
	write(t, neatFile, neat)
	write(t, skipped, messy)
	write(t, other, messy)

	// -l and -check say which, and change nothing
	code, out, _ := cmd("fmt", "-l", dir)
	if code != 0 || out != messyFile+"\n" {
		t.Errorf("-l: %d %q", code, out)
	}

	code, out, _ = cmd("fmt", "-check", dir)
	if code != 1 || out != messyFile+"\n" {
		t.Errorf("-check: %d %q", code, out)
	}

	if read(t, messyFile) != messy {
		t.Error("-l and -check do not write")
	}

	// -w writes it, and then there is nothing to say
	code, out, errs := cmd("fmt", "-w", dir)
	if code != 0 || out != "" || errs != "" {
		t.Errorf("-w: %d %q %q", code, out, errs)
	}

	if read(t, messyFile) != neat || read(t, neatFile) != neat {
		t.Errorf("-w: %q", read(t, messyFile))
	}

	if read(t, skipped) != messy || read(t, other) != messy {
		t.Error("a directory that starts with _, and a file that is not .cov, are left alone")
	}

	if code, out, _ = cmd("fmt", "-check", dir); code != 0 || out != "" {
		t.Errorf("-check after -w: %d %q", code, out)
	}
}

func TestFmtLeavesAFileThatIsNotCovenant(t *testing.T) {
	dir := t.TempDir()
	broken, fine := filepath.Join(dir, "a_broken.cov"), filepath.Join(dir, "b_fine.cov")

	write(t, broken, "fn ( {\n")
	write(t, fine, messy)

	code, _, errs := cmd("fmt", "-w", dir)
	if code != 1 || !strings.Contains(errs, "a_broken.cov: 1:") {
		t.Errorf("a mistake in a file: %d %q", code, errs)
	}

	if read(t, broken) != "fn ( {\n" {
		t.Error("the file with a mistake is as it was")
	}

	if read(t, fine) != neat {
		t.Error("the others are still formatted")
	}
}

func TestFmtUsage(t *testing.T) {
	dir := t.TempDir()

	for _, args := range [][]string{{"fmt"}, {"fmt", "a.cov", "b.cov"}, {"fmt", dir}} {
		if code, _, errs := cmd(args...); code != 2 || errs == "" {
			t.Errorf("%v: %d %q", args, code, errs)
		}
	}

	if code, _, errs := cmd("fmt", filepath.Join(dir, "missing.cov")); code != 1 || !strings.Contains(errs, "missing.cov") {
		t.Errorf("a file that is not there: %d %q", code, errs)
	}
}
