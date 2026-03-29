package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/joebhakim/themer/internal/config"
)

type Manager struct {
	cfg              *config.Config
	order            []string
	adapters         map[string]Adapter
	mu               sync.Mutex
	previewOrder     []string
	previewRestore   map[string]func(context.Context) error
	latestPreviewSeq int
}

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
	return &Manager{
		cfg:            cfg,
		order:          order,
		adapters:       index,
		previewRestore: map[string]func(context.Context) error{},
	}, nil
}

func (m *Manager) Config() *config.Config {
	return m.cfg
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
		out = append(out, adapter.Validate(ctx)...)
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
		current, err := adapter.Current(ctx)
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
		err := adapter.Apply(ctx, theme)
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
		current, err := adapter.Current(ctx)
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
			current, err := adapter.Current(ctx)
			if err != nil {
				detail.Error = err.Error()
			} else {
				detail.Current = current
			}
			description, err := adapter.Describe(ctx, theme)
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

func (m *Manager) PreviewProfile(ctx context.Context, profileName string, seq int) error {
	profile, ok := m.cfg.Profiles[profileName]
	if !ok {
		return fmt.Errorf("unknown profile %q", profileName)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if seq < m.latestPreviewSeq {
		return nil
	}
	m.latestPreviewSeq = seq
	m.restoreLocked(ctx)

	for _, adapter := range m.enabledAdapters() {
		theme, ok := profile.Targets[adapter.Name()]
		if !ok {
			continue
		}
		if support := adapter.PreviewStatus(ctx); !support.Enabled {
			continue
		}
		restore, err := adapter.Preview(ctx, theme)
		if err != nil || restore == nil {
			continue
		}
		m.previewRestore[adapter.Name()] = restore
		m.previewOrder = append(m.previewOrder, adapter.Name())
	}
	return nil
}

func (m *Manager) RestorePreview(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latestPreviewSeq++
	return m.restoreLocked(ctx)
}

func (m *Manager) restoreLocked(ctx context.Context) error {
	var errs []string
	for idx := len(m.previewOrder) - 1; idx >= 0; idx-- {
		name := m.previewOrder[idx]
		restore := m.previewRestore[name]
		if restore == nil {
			continue
		}
		if err := restore(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}
	m.previewOrder = nil
	m.previewRestore = map[string]func(context.Context) error{}
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
