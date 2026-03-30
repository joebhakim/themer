package tracing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/joebhakim/themer/internal/config"
)

type Event struct {
	Time       time.Time `json:"time"`
	Kind       string    `json:"kind"`
	Adapter    string    `json:"adapter,omitempty"`
	Operation  string    `json:"operation,omitempty"`
	Profile    string    `json:"profile,omitempty"`
	Theme      string    `json:"theme,omitempty"`
	Stage      string    `json:"stage,omitempty"`
	Status     string    `json:"status,omitempty"`
	Message    string    `json:"message,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Sequence   int       `json:"sequence,omitempty"`
	Command    string    `json:"command,omitempty"`
	Executable string    `json:"executable,omitempty"`
	ExitCode   *int      `json:"exit_code,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type Recorder interface {
	Enabled() bool
	Path() string
	Record(Event)
	Close() error
}

type disabledRecorder struct{}

func Disabled() Recorder {
	return disabledRecorder{}
}

func (disabledRecorder) Enabled() bool { return false }
func (disabledRecorder) Path() string  { return "" }
func (disabledRecorder) Record(Event)  {}
func (disabledRecorder) Close() error  { return nil }

type JSONLRecorder struct {
	path string
	file *os.File
	enc  *json.Encoder
	mu   sync.Mutex
}

func NewSession(path string) (Recorder, error) {
	tracePath := path
	if tracePath == "" {
		if err := os.MkdirAll(config.DefaultRuntimeDir(), 0o755); err != nil {
			return nil, fmt.Errorf("create runtime dir: %w", err)
		}
		tracePath = filepath.Join(
			config.DefaultRuntimeDir(),
			fmt.Sprintf("trace-%s-%d.jsonl", time.Now().Format("20060102-150405"), os.Getpid()),
		)
	} else {
		if err := os.MkdirAll(filepath.Dir(tracePath), 0o755); err != nil {
			return nil, fmt.Errorf("create trace dir: %w", err)
		}
	}

	file, err := os.Create(tracePath)
	if err != nil {
		return nil, fmt.Errorf("create trace file %s: %w", tracePath, err)
	}
	return &JSONLRecorder{
		path: tracePath,
		file: file,
		enc:  json.NewEncoder(file),
	}, nil
}

func (r *JSONLRecorder) Enabled() bool {
	return r != nil && r.file != nil
}

func (r *JSONLRecorder) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

func (r *JSONLRecorder) Record(event Event) {
	if !r.Enabled() {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.enc.Encode(event)
}

func (r *JSONLRecorder) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	err := r.file.Close()
	r.file = nil
	return err
}
