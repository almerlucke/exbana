package std

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"time"

	"github.com/almerlucke/exbana/v3/examples/covenant/run"
)

func (s *Std) envHost(h map[string]run.HostFunc) {
	h["env::get"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		name := a[0].(string)

		if !slices.Contains(s.policy.EnvVars, "*") && !slices.Contains(s.policy.EnvVars, name) {
			return s.denied("env", "EnvError"), nil
		}

		v, ok := os.LookupEnv(name)
		if !ok {
			return s.fail("env", "EnvError", "NotSet"), nil
		}

		return s.prog.Ok(v), nil
	}
}

// limitBuffer is a buffer that stops taking what is written when it is full, and says that it was. It only has Write: a buffer
// that is embedded would bring its ReadFrom, and io.Copy would use that and not keep to the limit.
type limitBuffer struct {
	buf  bytes.Buffer
	max  int64
	over bool
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	n := len(p)

	if room := b.max - int64(b.buf.Len()); int64(len(p)) > room {
		b.over = true
		p = p[:max(room, 0)]
	}

	b.buf.Write(p)

	return n, nil
}

func (b *limitBuffer) String() string { return b.buf.String() }

func (s *Std) processHost(h map[string]run.HostFunc) {
	h["process::run"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		program := a[0].(string)

		if !slices.Contains(s.policy.Programs, "*") && !slices.Contains(s.policy.Programs, program) {
			return s.denied("process", "ProcessError"), nil
		}

		path, err := exec.LookPath(program)
		if err != nil {
			return s.fail("process", "ProcessError", "Failed", "there is no program "+program), nil
		}

		items := a[1].(*run.List).Items()
		args := make([]string, len(items))

		for i, x := range items {
			args[i] = x.(string)
		}

		cctx, cancel := context.WithTimeout(ctx, s.policy.timeout())
		defer cancel()

		// the program has what it needs to be found, and nothing else of the environment of this one
		cmd := exec.CommandContext(cctx, path, args...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
		cmd.WaitDelay = time.Second

		stdout, stderr := &limitBuffer{max: s.policy.maxData()}, &limitBuffer{max: s.policy.maxData()}
		cmd.Stdout, cmd.Stderr = stdout, stderr

		err = cmd.Run()

		var exit *exec.ExitError

		switch {
		case ctx.Err() != nil:
			return nil, &run.Limit{Kind: "cancelled", Err: ctx.Err()}
		case errors.Is(cctx.Err(), context.DeadlineExceeded):
			return s.fail("process", "ProcessError", "Failed", "the program ran too long"), nil
		case err != nil && !errors.As(err, &exit):
			return s.fail("process", "ProcessError", "Failed", err.Error()), nil
		case stdout.over || stderr.over:
			return s.fail("process", "ProcessError", "Failed", "the program wrote more than the most that is read"), nil
		}

		code := 0
		if exit != nil {
			code = exit.ExitCode()
		}

		out, err := s.prog.Struct("process", "Output", nil, int64(code), stdout.String(), stderr.String())
		if err != nil {
			return nil, err
		}

		return s.prog.Ok(out), nil
	}
}
