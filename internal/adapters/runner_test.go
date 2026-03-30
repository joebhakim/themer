package adapters

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joebhakim/themer/internal/tracing"
)

type fixedRunner struct {
	result CommandResult
	err    error
}

func (r fixedRunner) Run(context.Context, string, ...string) (CommandResult, error) {
	return r.result, r.err
}

func TestInstrumentedRunnerWritesCommandTrace(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "commands.jsonl")
	recorder, err := tracing.NewSession(tracePath)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	runner := NewInstrumentedRunner(fixedRunner{
		result: CommandResult{ExitCode: 0},
	}, recorder)
	if _, err := runner.Run(context.Background(), "fish", "-c", "echo 'hello'\nexit 0"); err != nil {
		t.Fatalf("run command: %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("close recorder: %v", err)
	}

	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace file: %v", err)
	}
	lines := bytesLines(data)
	if len(lines) != 2 {
		t.Fatalf("line count = %d, want 2", len(lines))
	}

	var start, end tracing.Event
	if err := json.Unmarshal(lines[0], &start); err != nil {
		t.Fatalf("unmarshal start event: %v", err)
	}
	if err := json.Unmarshal(lines[1], &end); err != nil {
		t.Fatalf("unmarshal end event: %v", err)
	}
	if start.Stage != "start" || end.Stage != "end" {
		t.Fatalf("stages = %q/%q, want start/end", start.Stage, end.Stage)
	}
	if start.Command != "fish -c <multiline len=19>" {
		t.Fatalf("start command = %q", start.Command)
	}
	if end.Status != "ok" {
		t.Fatalf("end status = %q, want ok", end.Status)
	}
}

func bytesLines(data []byte) [][]byte {
	data = bytesTrimSpace(data)
	if len(data) == 0 {
		return nil
	}
	return splitLines(data)
}

func bytesTrimSpace(data []byte) []byte {
	start := 0
	for start < len(data) && (data[start] == '\n' || data[start] == '\r' || data[start] == ' ' || data[start] == '\t') {
		start++
	}
	end := len(data)
	for end > start && (data[end-1] == '\n' || data[end-1] == '\r' || data[end-1] == ' ' || data[end-1] == '\t') {
		end--
	}
	return data[start:end]
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for idx, b := range data {
		if b != '\n' {
			continue
		}
		lines = append(lines, append([]byte(nil), data[start:idx]...))
		start = idx + 1
	}
	if start < len(data) {
		lines = append(lines, append([]byte(nil), data[start:]...))
	}
	return lines
}
