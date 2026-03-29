package core

import "context"

type PaletteEntry struct {
	Label string
	Value string
}

type ThemeDescription struct {
	Summary string
	Palette []PaletteEntry
	Notes   []string
}

type PreviewSupport struct {
	Enabled bool
	Reason  string
}

type Diagnostic struct {
	Adapter string
	Level   string
	Message string
}

type Adapter interface {
	Name() string
	DisplayName() string
	Validate(context.Context) []Diagnostic
	ListThemes(context.Context) ([]string, error)
	Current(context.Context) (string, error)
	Describe(context.Context, string) (ThemeDescription, error)
	PreviewStatus(context.Context) PreviewSupport
	Preview(context.Context, string) (func(context.Context) error, error)
	Apply(context.Context, string) error
}

type ApplyResult struct {
	Adapter string
	Theme   string
	Err     error
	Skipped bool
}

type CurrentResult struct {
	Adapter string `json:"adapter"`
	Display string `json:"display"`
	Theme   string `json:"theme,omitempty"`
	Error   string `json:"error,omitempty"`
}

type AdapterDetail struct {
	Adapter        string
	Display        string
	Theme          string
	Current        string
	Description    ThemeDescription
	PreviewSupport PreviewSupport
	Error          string
}
