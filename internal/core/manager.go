package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joebhakim/themer/internal/config"
	"github.com/joebhakim/themer/internal/tracing"
)

type Manager struct {
	cfg              *config.Config
	order            []string
	adapters         map[string]Adapter
	previewReqMu     sync.Mutex
	pendingPreview   *previewIntent
	latestPreviewSeq int
	minPreviewSeq    int
	previewWake      chan struct{}
	previewCtrl      chan previewControl
	workerStateMu    sync.Mutex
	workerState      previewWorkerRuntime
	activityMu       sync.Mutex
	activity         []ActivityEntry
	activityCh       chan ActivityEntry
	recorder         tracing.Recorder
}

type previewIntent struct {
	profile string
	seq     int
}

type previewControlKind int

const (
	previewControlFlush previewControlKind = iota
	previewControlRestore
	previewControlCommit
	previewControlSyncPreview
	previewControlWaitIdle
)

type previewControl struct {
	kind    previewControlKind
	ctx     context.Context
	profile string
	seq     int
	resp    chan error
}

type previewWorkerState struct {
	previewOrder   []string
	previewRestore map[string]func(context.Context) error
	activeProfile  string
	activeSeq      int
}

type previewWorkerRuntime struct {
	busy    bool
	stage   string
	profile string
	adapter string
	seq     int
}

var (
	activityLatencyThreshold = 250 * time.Millisecond
	operationHeartbeatStart  = 500 * time.Millisecond
	operationHeartbeatEvery  = time.Second
	currentOperationTimeout  = 1500 * time.Millisecond
	describeOperationTimeout = 2 * time.Second
	previewOperationTimeout  = time.Second
	restoreOperationTimeout  = time.Second
	applyOperationTimeout    = 12 * time.Second
	commitControlTimeout     = 150 * time.Millisecond
)

func NewManager(cfg *config.Config, adapters []Adapter) (*Manager, error) {
	index := make(map[string]Adapter, len(adapters))
	order := make([]string, 0, len(adapters))
	for _, adapter := range adapters {
		index[adapter.Name()] = adapter
		order = append(order, adapter.Name())
	}
	for _, name := range cfg.EnabledAdapters {
		if _, ok := index[name]; !ok {
			return nil, fmt.Errorf("enabled adapter %q is not registered", name)
		}
	}
	manager := &Manager{
		cfg:         cfg,
		order:       order,
		adapters:    index,
		previewWake: make(chan struct{}, 1),
		previewCtrl: make(chan previewControl),
		activityCh:  make(chan ActivityEntry, 128),
		recorder:    tracing.Disabled(),
	}
	for _, adapter := range adapters {
		if activityAware, ok := adapter.(ActivityAwareAdapter); ok {
			activityAware.SetActivityLogger(manager.logActivity)
		}
	}
	go manager.previewLoop()
	return manager, nil
}

func (m *Manager) Config() *config.Config {
	return m.cfg
}

func (m *Manager) SetTraceRecorder(recorder tracing.Recorder) {
	if recorder == nil {
		recorder = tracing.Disabled()
	}
	m.recorder = recorder
	if path := recorder.Path(); path != "" {
		m.logActivity(ActivityEntry{
			Adapter: "themer",
			Stage:   "trace",
			Message: "trace enabled: " + path,
		})
	}
}

func (m *Manager) TracePath() string {
	if m.recorder == nil {
		return ""
	}
	return m.recorder.Path()
}

func (m *Manager) Close() error {
	if m.recorder == nil {
		return nil
	}
	return m.recorder.Close()
}

func (m *Manager) ProfileNames() []string {
	return m.cfg.ProfileNames()
}

func (m *Manager) EnabledAdapters() []Adapter {
	return m.enabledAdapters()
}

func (m *Manager) ProfileTargets(name string) (map[string]string, bool) {
	profile, ok := m.cfg.Profiles[name]
	if !ok {
		return nil, false
	}
	targets := make(map[string]string, len(profile.Targets))
	for key, value := range profile.Targets {
		targets[key] = value
	}
	return targets, true
}

func (m *Manager) Diagnostics(ctx context.Context) []Diagnostic {
	var out []Diagnostic
	for _, adapter := range m.enabledAdapters() {
		diagnostics, err := observeAdapterValue(ctx, m, adapter.Name(), adapter.DisplayName(), "validate", "", "", 0, func(ctx context.Context) ([]Diagnostic, error) {
			return adapter.Validate(ctx), nil
		})
		if err != nil {
			m.logActivity(ActivityEntry{
				Adapter: adapter.DisplayName(),
				Stage:   "timing",
				Message: fmt.Sprintf("validate request failed: %s", errorString(err)),
			})
			continue
		}
		out = append(out, diagnostics...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Adapter == out[j].Adapter {
			return out[i].Message < out[j].Message
		}
		return out[i].Adapter < out[j].Adapter
	})
	return out
}

func (m *Manager) Current(ctx context.Context) []CurrentResult {
	results := make([]CurrentResult, 0, len(m.order))
	for _, adapter := range m.enabledAdapters() {
		opCtx, cancel := withOperationTimeout(ctx, currentOperationTimeout)
		current, err := observeAdapterValue(opCtx, m, adapter.Name(), adapter.DisplayName(), "current", "", "", 0, func(ctx context.Context) (string, error) {
			return adapter.Current(ctx)
		})
		cancel()
		item := CurrentResult{
			Adapter: adapter.Name(),
			Display: adapter.DisplayName(),
			Theme:   current,
		}
		if err != nil {
			item.Error = err.Error()
			item.Theme = ""
		}
		results = append(results, item)
	}
	return results
}

func (m *Manager) ApplyProfile(ctx context.Context, profileName string) ([]ApplyResult, error) {
	profile, ok := m.cfg.Profiles[profileName]
	if !ok {
		return nil, fmt.Errorf("unknown profile %q", profileName)
	}
	results := make([]ApplyResult, 0, len(m.order))
	var errs []string
	for _, adapter := range m.enabledAdapters() {
		theme, ok := profile.Targets[adapter.Name()]
		if !ok {
			results = append(results, ApplyResult{
				Adapter: adapter.DisplayName(),
				Skipped: true,
			})
			continue
		}
		opCtx, cancel := withOperationTimeout(ctx, applyOperationTimeout)
		err := observeAdapterAction(opCtx, m, adapter.Name(), adapter.DisplayName(), "apply", profileName, theme, 0, func(ctx context.Context) error {
			return adapter.Apply(ctx, theme)
		})
		cancel()
		results = append(results, ApplyResult{
			Adapter: adapter.DisplayName(),
			Theme:   theme,
			Err:     err,
		})
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", adapter.DisplayName(), err))
		}
	}
	if len(errs) > 0 {
		return results, errors.New(strings.Join(errs, "; "))
	}
	return results, nil
}

func (m *Manager) CaptureCurrent(ctx context.Context) (map[string]string, error) {
	targets := map[string]string{}
	var errs []string
	for _, adapter := range m.enabledAdapters() {
		opCtx, cancel := withOperationTimeout(ctx, currentOperationTimeout)
		current, err := observeAdapterValue(opCtx, m, adapter.Name(), adapter.DisplayName(), "capture-current", "", "", 0, func(ctx context.Context) (string, error) {
			return adapter.Current(ctx)
		})
		cancel()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", adapter.DisplayName(), err))
			continue
		}
		if strings.TrimSpace(current) == "" {
			continue
		}
		targets[adapter.Name()] = current
	}
	if len(targets) == 0 && len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return targets, nil
}

func (m *Manager) DescribeProfile(ctx context.Context, profileName string) ([]AdapterDetail, error) {
	profile, ok := m.cfg.Profiles[profileName]
	if !ok {
		return nil, fmt.Errorf("unknown profile %q", profileName)
	}
	details := make([]AdapterDetail, 0, len(m.order))
	for _, adapter := range m.enabledAdapters() {
		detail := AdapterDetail{
			Adapter:        adapter.Name(),
			Display:        adapter.DisplayName(),
			PreviewSupport: adapter.PreviewStatus(ctx),
		}
		if theme, ok := profile.Targets[adapter.Name()]; ok {
			detail.Theme = theme
			currentCtx, cancelCurrent := withOperationTimeout(ctx, currentOperationTimeout)
			current, err := observeAdapterValue(currentCtx, m, adapter.Name(), adapter.DisplayName(), "describe-current", profileName, theme, 0, func(ctx context.Context) (string, error) {
				return adapter.Current(ctx)
			})
			cancelCurrent()
			if err != nil {
				detail.Error = err.Error()
			} else {
				detail.Current = current
			}
			describeCtx, cancelDescribe := withOperationTimeout(ctx, describeOperationTimeout)
			description, err := observeAdapterValue(describeCtx, m, adapter.Name(), adapter.DisplayName(), "describe", profileName, theme, 0, func(ctx context.Context) (ThemeDescription, error) {
				return adapter.Describe(ctx, theme)
			})
			cancelDescribe()
			if err != nil {
				if detail.Error == "" {
					detail.Error = err.Error()
				} else {
					detail.Error += "; " + err.Error()
				}
			} else {
				detail.Description = description
			}
		}
		details = append(details, detail)
	}
	return details, nil
}

func (m *Manager) QueuePreview(profileName string, seq int) {
	if profileName == "" {
		return
	}
	var replaced *previewIntent
	m.previewReqMu.Lock()
	switch {
	case seq < m.minPreviewSeq:
		m.previewReqMu.Unlock()
		m.workerActivity("preview", fmt.Sprintf("ignored stale preview %s (seq %d < barrier %d)", profileName, seq, m.minPreviewSeq))
		return
	case seq <= m.latestPreviewSeq:
		m.previewReqMu.Unlock()
		m.workerActivity("preview", fmt.Sprintf("ignored stale preview %s (seq %d <= latest %d)", profileName, seq, m.latestPreviewSeq))
		return
	default:
		if pending := m.pendingPreview; pending != nil {
			copyPending := *pending
			replaced = &copyPending
		}
		m.latestPreviewSeq = seq
		m.pendingPreview = &previewIntent{profile: profileName, seq: seq}
	}
	m.previewReqMu.Unlock()

	if replaced != nil {
		m.workerActivity("preview", fmt.Sprintf("preview %s (seq %d) superseded before start by %s (seq %d)", replaced.profile, replaced.seq, profileName, seq))
	}
	if runtime := m.snapshotWorkerRuntime(); runtime.busy {
		m.workerActivity("preview", fmt.Sprintf("queued preview %s (seq %d); worker busy %s", profileName, seq, runtime.describe()))
	} else {
		m.workerActivity("preview", fmt.Sprintf("queued preview %s (seq %d); worker idle", profileName, seq))
	}
	m.signalPreviewWorker()
}

func (m *Manager) PreviewProfile(ctx context.Context, profileName string, seq int) error {
	if profileName == "" {
		return nil
	}
	m.previewReqMu.Lock()
	if seq < m.minPreviewSeq || seq <= m.latestPreviewSeq {
		m.previewReqMu.Unlock()
		return nil
	}
	m.latestPreviewSeq = seq
	m.previewReqMu.Unlock()
	return m.sendPreviewControl(ctx, previewControl{
		kind:    previewControlSyncPreview,
		ctx:     ctx,
		profile: profileName,
		seq:     seq,
	})
}

func (m *Manager) CancelPendingPreview() {
	if pending := m.invalidatePendingPreview(); pending != nil {
		m.workerActivity("preview", fmt.Sprintf("pending preview canceled for %s (seq %d)", pending.profile, pending.seq))
	}
}

func (m *Manager) RestorePreview(ctx context.Context) error {
	if pending := m.invalidatePendingPreview(); pending != nil {
		m.workerActivity("preview", fmt.Sprintf("pending preview canceled before restore for %s (seq %d)", pending.profile, pending.seq))
	}
	m.workerActivity("restore", "restore requested; waiting for preview worker")
	return m.sendPreviewControl(ctx, previewControl{
		kind: previewControlRestore,
		ctx:  ctx,
	})
}

func (m *Manager) FlushPreview(ctx context.Context) error {
	if pending := m.invalidatePendingPreview(); pending != nil {
		m.workerActivity("preview", fmt.Sprintf("pending preview canceled before flush for %s (seq %d)", pending.profile, pending.seq))
	}
	m.workerActivity("flush", "flush requested; waiting for preview worker")
	return m.sendPreviewControl(ctx, previewControl{
		kind: previewControlFlush,
		ctx:  ctx,
	})
}

func (m *Manager) CommitPreview() {
	m.invalidatePendingPreview()
	ctx, cancel := context.WithTimeout(context.Background(), commitControlTimeout)
	defer cancel()
	if err := m.sendPreviewControl(ctx, previewControl{
		kind: previewControlCommit,
		ctx:  ctx,
	}); err != nil {
		m.workerActivity("commit", "preview commit deferred: "+err.Error())
	}
}

func (m *Manager) ActivityLog() []ActivityEntry {
	m.activityMu.Lock()
	defer m.activityMu.Unlock()
	out := make([]ActivityEntry, len(m.activity))
	copy(out, m.activity)
	return out
}

func (m *Manager) ActivityStream() <-chan ActivityEntry {
	return m.activityCh
}

func (m *Manager) waitPreviewIdle(ctx context.Context) error {
	return m.sendPreviewControl(ctx, previewControl{
		kind: previewControlWaitIdle,
		ctx:  ctx,
	})
}

func (m *Manager) restoreLocked(ctx context.Context, state *previewWorkerState) error {
	var errs []string
	for idx := len(state.previewOrder) - 1; idx >= 0; idx-- {
		name := state.previewOrder[idx]
		restore := state.previewRestore[name]
		if restore == nil {
			continue
		}
		adapter := m.adapters[name]
		display := name
		if adapter != nil {
			display = adapter.DisplayName()
		}
		m.workerActivity("restore", fmt.Sprintf("calling restore for %s", display))
		restoreCtx, cancel := withOperationTimeout(ctx, restoreOperationTimeout)
		err := observeAdapterAction(restoreCtx, m, name, display, "restore", state.activeProfile, "", state.activeSeq, restore)
		cancel()
		if err != nil {
			m.workerActivity("restore", fmt.Sprintf("restore failed for %s: %v", display, err))
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		m.workerActivity("restore", fmt.Sprintf("restore returned for %s", display))
	}
	state.previewOrder = nil
	state.previewRestore = map[string]func(context.Context) error{}
	state.activeProfile = ""
	state.activeSeq = 0
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) enabledAdapters() []Adapter {
	adapters := make([]Adapter, 0, len(m.cfg.EnabledAdapters))
	for _, name := range m.cfg.EnabledAdapters {
		adapters = append(adapters, m.adapters[name])
	}
	return adapters
}

func (m *Manager) logActivity(entry ActivityEntry) {
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	m.activityMu.Lock()
	m.activity = append(m.activity, entry)
	if len(m.activity) > 80 {
		m.activity = append([]ActivityEntry(nil), m.activity[len(m.activity)-80:]...)
	}
	m.activityMu.Unlock()
	if m.recorder != nil && m.recorder.Enabled() {
		m.recorder.Record(tracing.Event{
			Time:    entry.Time,
			Kind:    "activity",
			Adapter: entry.Adapter,
			Stage:   entry.Stage,
			Message: entry.Message,
		})
	}
	select {
	case m.activityCh <- entry:
	default:
	}
}

func observeAdapterAction(ctx context.Context, m *Manager, adapterName, display, operation, profile, theme string, seq int, fn func(context.Context) error) error {
	_, err := observeAdapterValue(ctx, m, adapterName, display, operation, profile, theme, seq, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return err
}

func observeAdapterValue[T any](ctx context.Context, m *Manager, adapterName, display, operation, profile, theme string, seq int, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	if m == nil {
		return fn(ctx)
	}

	start := time.Now()
	m.recordTrace(tracing.Event{
		Kind:      "adapter_op",
		Adapter:   adapterName,
		Operation: operation,
		Profile:   profile,
		Theme:     theme,
		Stage:     "start",
		Sequence:  seq,
	})

	done := make(chan struct{})
	go m.emitSlowOperationSignals(done, ctx, adapterName, display, operation, profile, theme, seq, start)
	value, err := fn(ctx)
	close(done)

	duration := time.Since(start)
	m.recordTrace(tracing.Event{
		Kind:       "adapter_op",
		Adapter:    adapterName,
		Operation:  operation,
		Profile:    profile,
		Theme:      theme,
		Stage:      "end",
		Status:     operationStatus(err),
		DurationMS: duration.Milliseconds(),
		Sequence:   seq,
		Error:      errorString(err),
	})
	if duration >= activityLatencyThreshold || err != nil {
		m.logActivity(ActivityEntry{
			Adapter: display,
			Stage:   "timing",
			Message: operationSummary(operation, profile, theme, duration, err),
		})
	}
	if err != nil {
		return zero, err
	}
	return value, nil
}

func (m *Manager) emitSlowOperationSignals(done <-chan struct{}, ctx context.Context, adapterName, display, operation, profile, theme string, seq int, start time.Time) {
	timer := time.NewTimer(operationHeartbeatStart)
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
	}

	ticker := time.NewTicker(operationHeartbeatEvery)
	defer ticker.Stop()
	for {
		elapsed := time.Since(start)
		m.logActivity(ActivityEntry{
			Adapter: display,
			Stage:   "timing",
			Message: fmt.Sprintf("%s %s still running (%s)", operation, operationContext(profile, theme), formatTiming(elapsed)),
		})
		m.recordTrace(tracing.Event{
			Kind:       "adapter_op",
			Adapter:    adapterName,
			Operation:  operation,
			Profile:    profile,
			Theme:      theme,
			Stage:      "heartbeat",
			Status:     heartbeatStatus(ctx),
			DurationMS: elapsed.Milliseconds(),
			Sequence:   seq,
		})

		select {
		case <-done:
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) recordTrace(event tracing.Event) {
	if m.recorder == nil || !m.recorder.Enabled() {
		return
	}
	m.recorder.Record(event)
}

func operationStatus(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	default:
		return "error"
	}
}

func heartbeatStatus(ctx context.Context) string {
	if ctx != nil && ctx.Err() != nil {
		return "cancel_requested"
	}
	return "running"
}

func operationSummary(operation, profile, theme string, duration time.Duration, err error) string {
	context := operationContext(profile, theme)
	if err != nil {
		return fmt.Sprintf("%s %s failed after %s: %s", operation, context, formatTiming(duration), errorString(err))
	}
	return fmt.Sprintf("%s %s completed in %s", operation, context, formatTiming(duration))
}

func operationContext(profile, theme string) string {
	switch {
	case theme != "" && profile != "":
		return fmt.Sprintf("%s for %s", theme, profile)
	case theme != "":
		return theme
	case profile != "":
		return profile
	default:
		return "request"
	}
}

func formatTiming(duration time.Duration) string {
	return duration.Round(10 * time.Millisecond).String()
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	if message := strings.TrimSpace(err.Error()); message != "" {
		return message
	}
	return "unknown error"
}

func (m *Manager) previewLoop() {
	state := previewWorkerState{
		previewRestore: map[string]func(context.Context) error{},
	}
	m.clearWorkerRuntime()
	for {
		select {
		case control := <-m.previewCtrl:
			m.handlePreviewControl(control, &state)
			continue
		default:
		}

		if intent := m.takePendingPreview(); intent != nil {
			m.workerActivity("preview", fmt.Sprintf("worker picked queued preview %s (seq %d)", intent.profile, intent.seq))
			m.runPreviewIntent(context.Background(), *intent, &state)
			continue
		}

		m.clearWorkerRuntime()
		select {
		case control := <-m.previewCtrl:
			m.handlePreviewControl(control, &state)
		case <-m.previewWake:
		}
	}
}

func (m *Manager) handlePreviewControl(control previewControl, state *previewWorkerState) {
	ctx := control.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var err error
	switch control.kind {
	case previewControlFlush:
		m.setWorkerRuntime("flush", state.activeProfile, "", state.activeSeq)
		m.workerActivity("flush", "flush-before-apply start")
		err = m.restoreActivePreview(ctx, state, "flush")
		if err == nil {
			m.workerActivity("flush", "flush complete")
		} else {
			m.workerActivity("flush", "flush failed: "+err.Error())
		}
	case previewControlRestore:
		m.setWorkerRuntime("restore", state.activeProfile, "", state.activeSeq)
		m.workerActivity("restore", "restore requested")
		err = m.restoreActivePreview(ctx, state, "restore")
		if err == nil {
			m.workerActivity("restore", "restore complete")
		} else {
			m.workerActivity("restore", "restore failed: "+err.Error())
		}
	case previewControlCommit:
		m.setWorkerRuntime("commit", state.activeProfile, "", state.activeSeq)
		if state.activeProfile != "" {
			m.workerActivity("commit", fmt.Sprintf("committed preview state for %s", state.activeProfile))
		}
		state.previewOrder = nil
		state.previewRestore = map[string]func(context.Context) error{}
		state.activeProfile = ""
		state.activeSeq = 0
	case previewControlSyncPreview:
		err = m.runPreviewIntent(ctx, previewIntent{profile: control.profile, seq: control.seq}, state)
	case previewControlWaitIdle:
		for {
			intent := m.takePendingPreview()
			if intent == nil {
				break
			}
			m.workerActivity("preview", fmt.Sprintf("worker draining queued preview %s (seq %d) before idle wait", intent.profile, intent.seq))
			if runErr := m.runPreviewIntent(ctx, *intent, state); err == nil {
				err = runErr
			}
		}
		m.workerActivity("preview", "worker idle")
	}
	m.clearWorkerRuntime()
	if control.resp != nil {
		control.resp <- err
	}
}

func (m *Manager) runPreviewIntent(ctx context.Context, intent previewIntent, state *previewWorkerState) error {
	profile, ok := m.cfg.Profiles[intent.profile]
	if !ok {
		err := fmt.Errorf("unknown profile %q", intent.profile)
		m.workerActivity("preview", err.Error())
		return err
	}
	m.setWorkerRuntime("preview-start", intent.profile, "", intent.seq)
	if state.activeProfile != "" {
		m.setWorkerRuntime("preview-restore", state.activeProfile, "", state.activeSeq)
		m.workerActivity("preview", fmt.Sprintf("restoring previous preview %s before %s", state.activeProfile, intent.profile))
		if err := m.restoreActivePreview(ctx, state, "preview"); err != nil {
			m.workerActivity("preview", "restore before preview failed: "+err.Error())
		}
	}

	state.previewOrder = nil
	state.previewRestore = map[string]func(context.Context) error{}
	m.workerActivity("preview", fmt.Sprintf("starting preview %s (seq %d)", intent.profile, intent.seq))
	for _, adapter := range m.enabledAdapters() {
		theme, ok := profile.Targets[adapter.Name()]
		if !ok {
			continue
		}
		support := adapter.PreviewStatus(ctx)
		if !support.Enabled {
			continue
		}
		m.setWorkerRuntime("preview-call", intent.profile, adapter.DisplayName(), intent.seq)
		m.workerActivity("preview", fmt.Sprintf("calling preview adapter %s -> %s; waiting for adapter to return", adapter.DisplayName(), theme))
		previewCtx, cancel := withOperationTimeout(ctx, previewOperationTimeout)
		restore, err := observeAdapterValue(previewCtx, m, adapter.Name(), adapter.DisplayName(), "preview", intent.profile, theme, intent.seq, func(ctx context.Context) (func(context.Context) error, error) {
			return adapter.Preview(ctx, theme)
		})
		cancel()
		if err != nil {
			m.workerActivity("preview", fmt.Sprintf("preview adapter %s failed for %s: %v", adapter.DisplayName(), theme, err))
			m.logActivity(ActivityEntry{
				Adapter: adapter.DisplayName(),
				Stage:   "preview-error",
				Message: err.Error(),
			})
			continue
		}
		if restore == nil {
			m.workerActivity("preview", fmt.Sprintf("%s did not return restore state", adapter.DisplayName()))
			continue
		}
		state.previewRestore[adapter.Name()] = restore
		state.previewOrder = append(state.previewOrder, adapter.Name())
		m.workerActivity("preview", fmt.Sprintf("preview adapter %s returned; preview applied for %s", adapter.DisplayName(), theme))
	}
	if len(state.previewOrder) == 0 {
		m.workerActivity("preview", fmt.Sprintf("preview %s had no preview-enabled targets", intent.profile))
		m.clearWorkerRuntime()
		return nil
	}
	state.activeProfile = intent.profile
	state.activeSeq = intent.seq

	if pending := m.peekPendingPreviewAfter(intent.seq); pending != nil {
		m.workerActivity("preview", fmt.Sprintf("preview %s (seq %d) completed but is superseded by %s (seq %d)", intent.profile, intent.seq, pending.profile, pending.seq))
		return nil
	}
	m.workerActivity("preview", fmt.Sprintf("preview settled on %s (seq %d)", intent.profile, intent.seq))
	return nil
}

func (m *Manager) restoreActivePreview(ctx context.Context, state *previewWorkerState, stage string) error {
	if len(state.previewOrder) == 0 {
		m.workerActivity(stage, "no active preview to restore")
		return nil
	}
	profile := state.activeProfile
	if profile == "" {
		profile = "unknown"
	}
	m.workerActivity(stage, fmt.Sprintf("restoring preview state for %s", profile))
	return m.restoreLocked(ctx, state)
}

func (m *Manager) invalidatePendingPreview() *previewIntent {
	m.previewReqMu.Lock()
	defer m.previewReqMu.Unlock()
	pending := m.pendingPreview
	m.pendingPreview = nil
	nextSeq := m.latestPreviewSeq + 1
	if nextSeq > m.minPreviewSeq {
		m.minPreviewSeq = nextSeq
	}
	return pending
}

func (m *Manager) takePendingPreview() *previewIntent {
	m.previewReqMu.Lock()
	defer m.previewReqMu.Unlock()
	if m.pendingPreview == nil {
		return nil
	}
	intent := *m.pendingPreview
	m.pendingPreview = nil
	return &intent
}

func (m *Manager) peekPendingPreviewAfter(seq int) *previewIntent {
	m.previewReqMu.Lock()
	defer m.previewReqMu.Unlock()
	if m.pendingPreview == nil || m.pendingPreview.seq <= seq {
		return nil
	}
	intent := *m.pendingPreview
	return &intent
}

func (m *Manager) sendPreviewControl(ctx context.Context, control previewControl) error {
	if ctx == nil {
		ctx = context.Background()
	}
	control.ctx = ctx
	control.resp = make(chan error, 1)
	select {
	case m.previewCtrl <- control:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-control.resp:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) signalPreviewWorker() {
	select {
	case m.previewWake <- struct{}{}:
	default:
	}
}

func (m *Manager) workerActivity(stage, message string) {
	m.logActivity(ActivityEntry{
		Adapter: "themer",
		Stage:   stage,
		Message: message,
	})
}

func (m *Manager) setWorkerRuntime(stage, profile, adapter string, seq int) {
	m.workerStateMu.Lock()
	defer m.workerStateMu.Unlock()
	m.workerState = previewWorkerRuntime{
		busy:    true,
		stage:   stage,
		profile: profile,
		adapter: adapter,
		seq:     seq,
	}
}

func (m *Manager) clearWorkerRuntime() {
	m.workerStateMu.Lock()
	defer m.workerStateMu.Unlock()
	m.workerState = previewWorkerRuntime{}
}

func (m *Manager) snapshotWorkerRuntime() previewWorkerRuntime {
	m.workerStateMu.Lock()
	defer m.workerStateMu.Unlock()
	return m.workerState
}

func (p previewWorkerRuntime) describe() string {
	if !p.busy {
		return "idle"
	}
	parts := []string{p.stage}
	if p.profile != "" {
		parts = append(parts, fmt.Sprintf("profile %s", p.profile))
	}
	if p.adapter != "" {
		parts = append(parts, fmt.Sprintf("adapter %s", p.adapter))
	}
	if p.seq != 0 {
		parts = append(parts, fmt.Sprintf("seq %d", p.seq))
	}
	return strings.Join(parts, ", ")
}

func withOperationTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}
