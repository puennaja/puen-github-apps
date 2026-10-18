package reviewworker

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Adapter exposes provider metadata and a review entry point. Real provider
// execution remains disabled; only package-local synthetic tests use runProcess.
type Adapter interface {
	Provider() string
	Model() string
	Review(context.Context, Request) (Response, error)
}

type disabledAdapter struct{ provider, model string }

func (a disabledAdapter) Provider() string { return a.provider }
func (a disabledAdapter) Model() string    { return a.model }
func (disabledAdapter) Review(context.Context, Request) (Response, error) {
	return Response{}, ErrUnavailable
}

// The constructors deliberately contain no executable path or activation flag.
// A separately reviewed change must verify OS isolation, entitlement and cost.
func NewCodexAdapter(model string) Adapter  { return disabledAdapter{"codex", model} }
func NewClaudeAdapter(model string) Adapter { return disabledAdapter{"claude-code", model} }

type commandSpec struct {
	executable string
	allowlist  []string
	args       []string
	workingDir string
	provider   string
	model      string
}

type limitedBuffer struct {
	bytes.Buffer
	max      int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		b.overflow = true
		return 0, ErrUnavailable
	}
	return b.Buffer.Write(p)
}

// runProcess is intentionally private. It exercises the transport with synthetic
// executables, but does not establish filesystem, memory or network isolation.
func runProcess(ctx context.Context, spec commandSpec, request Request) (Response, error) {
	input, err := EncodeRequest(request)
	if err != nil || !allowedExecutable(spec.executable, spec.allowlist) || spec.workingDir == "" || spec.provider == "" || spec.model == "" {
		return Response{}, ErrUnavailable
	}
	if !filepath.IsAbs(spec.workingDir) {
		return Response{}, ErrUnavailable
	}
	if info, err := os.Stat(spec.workingDir); err != nil || !info.IsDir() {
		return Response{}, ErrUnavailable
	}
	// No inherited environment, no extra file descriptors and no shell. This does
	// not prevent filesystem reads or network access without an external sandbox.
	limit := time.Duration(request.Limits.TimeoutMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, spec.executable, spec.args...)
	cmd.Dir = spec.workingDir
	cmd.Env = []string{"HOME=/nonexistent", "PATH=/usr/bin:/bin", "LANG=C", "TMPDIR=/nonexistent"}
	cmd.Stdin = bytes.NewReader(input)
	stdout := &limitedBuffer{max: MaxResponseBytes}
	stderr := &limitedBuffer{max: 4 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.ExtraFiles = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil || stdout.overflow || stderr.overflow || ctx.Err() != nil {
		return Response{}, ErrUnavailable
	}
	result, err := DecodeResponse(stdout.Bytes(), request, spec.provider, spec.model)
	if err != nil {
		return Response{}, ErrUnavailable
	}
	return result, nil
}

func allowedExecutable(executable string, allowlist []string) bool {
	if !filepath.IsAbs(executable) || len(allowlist) == 0 {
		return false
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return false
	}
	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
		return false
	}
	for _, item := range allowlist {
		if !filepath.IsAbs(item) {
			continue
		}
		approved, err := filepath.EvalSymlinks(item)
		if err == nil && approved == resolved {
			return true
		}
	}
	return false
}
