package adapters

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/joebhakim/themer/internal/tracing"
)

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type CommandRunner interface {
	Run(context.Context, string, ...string) (CommandResult, error)
}

type ExecRunner struct{}

type InstrumentedRunner struct {
	base     CommandRunner
	recorder tracing.Recorder
}

func NewInstrumentedRunner(base CommandRunner, recorder tracing.Recorder) CommandRunner {
	if base == nil {
		base = ExecRunner{}
	}
	if recorder == nil || !recorder.Enabled() {
		return base
	}
	return InstrumentedRunner{
		base:     base,
		recorder: recorder,
	}
}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := CommandResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return result, nil
	}
	return result, err
}

func (r InstrumentedRunner) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	start := time.Now()
	commandSummary := summarizeCommand(name, args)
	r.recorder.Record(tracing.Event{
		Kind:       "command",
		Stage:      "start",
		Command:    commandSummary,
		Executable: name,
	})

	result, err := r.base.Run(ctx, name, args...)
	status := traceStatus(err, result.ExitCode)
	exitCode := result.ExitCode
	event := tracing.Event{
		Kind:       "command",
		Stage:      "end",
		Status:     status,
		Command:    commandSummary,
		Executable: name,
		DurationMS: time.Since(start).Milliseconds(),
		ExitCode:   &exitCode,
	}
	if err != nil {
		event.Error = err.Error()
	}
	r.recorder.Record(event)
	return result, err
}

func traceStatus(err error, exitCode int) string {
	switch {
	case err == nil && exitCode == 0:
		return "ok"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case err != nil:
		return "error"
	default:
		return "exit_nonzero"
	}
}

func summarizeCommand(name string, args []string) string {
	parts := []string{name}
	for _, arg := range args {
		parts = append(parts, summarizeArg(arg))
	}
	return strings.Join(parts, " ")
}

func summarizeArg(arg string) string {
	if strings.Contains(arg, "\n") {
		return fmt.Sprintf("<multiline len=%d>", len(arg))
	}
	if len(arg) > 120 {
		return fmt.Sprintf("<arg len=%d>", len(arg))
	}
	return arg
}
