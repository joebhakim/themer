package adapters

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/joebhakim/themer/internal/config"
	"github.com/joebhakim/themer/internal/core"
)

const kittyRemoteProbeTimeout = 250 * time.Millisecond

type Kitty struct {
	cfg              config.KittyConfig
	runner           CommandRunner
	currentThemePath string
	configPath       string
}

type kittyRemoteEndpoint struct {
	socket string
	match  string
	reason string
}

type kittySocketCandidate struct {
	socket string
	label  string
}

type kittyConfigHints struct {
	allowRemoteControl string
	listenOn           string
}

func NewKitty(cfg config.KittyConfig, runner CommandRunner) *Kitty {
	return &Kitty{
		cfg:              cfg,
		runner:           runner,
		currentThemePath: filepath.Join(xdgConfigHome(), "kitty", "current-theme.conf"),
		configPath:       filepath.Join(xdgConfigHome(), "kitty", "kitty.conf"),
	}
}

func (k *Kitty) Name() string {
	return "kitty"
}

func (k *Kitty) DisplayName() string {
	return "Kitty"
}

func (k *Kitty) Validate(ctx context.Context) []core.Diagnostic {
	var diagnostics []core.Diagnostic
	if _, err := exec.LookPath("kitty"); err != nil {
		diagnostics = append(diagnostics, core.Diagnostic{
			Adapter: k.Name(),
			Level:   "error",
			Message: "kitty binary is not available",
		})
		return diagnostics
	}
	if support := k.PreviewStatus(ctx); !support.Enabled {
		diagnostics = append(diagnostics, core.Diagnostic{
			Adapter: k.Name(),
			Level:   "warn",
			Message: "preview disabled: " + support.Reason,
		})
	}
	return diagnostics
}

func (k *Kitty) ListThemes(context.Context) ([]string, error) {
	themes := append([]string(nil), k.cfg.KnownThemes...)
	return themes, nil
}

func (k *Kitty) Current(ctx context.Context) (string, error) {
	data, err := os.ReadFile(k.currentThemePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	content := string(data)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "## name:")), nil
		}
	}
	currentColors := parseKittyColors(content)
	if len(currentColors) == 0 {
		return "", nil
	}
	for _, theme := range k.cfg.KnownThemes {
		dump, err := k.dumpTheme(ctx, theme)
		if err != nil {
			continue
		}
		colors := parseKittyColors(dump)
		if colors["background"] == currentColors["background"] && colors["foreground"] == currentColors["foreground"] {
			return theme, nil
		}
	}
	return "", nil
}

func (k *Kitty) Describe(ctx context.Context, theme string) (core.ThemeDescription, error) {
	dump, err := k.dumpTheme(ctx, theme)
	if err != nil {
		return core.ThemeDescription{}, err
	}
	colors := parseKittyColors(dump)
	description := core.ThemeDescription{
		Summary: "Kitty theme applied via kitten themes.",
	}
	for _, key := range []string{"background", "foreground", "cursor", "color0", "color1", "color2", "color3", "color4"} {
		if value := colors[key]; value != "" {
			description.Palette = append(description.Palette, core.PaletteEntry{Label: key, Value: value})
		}
	}
	return description, nil
}

func (k *Kitty) PreviewStatus(ctx context.Context) core.PreviewSupport {
	if _, err := exec.LookPath("kitty"); err != nil {
		return core.PreviewSupport{Reason: "kitty not found"}
	}
	endpoint, reason := k.previewEndpoint(ctx)
	if reason != "" {
		return core.PreviewSupport{Reason: reason}
	}
	return core.PreviewSupport{Enabled: true, Reason: endpoint.reason}
}

func (k *Kitty) Preview(ctx context.Context, theme string) (func(context.Context) error, error) {
	endpoint, reason := k.previewEndpoint(ctx)
	if reason != "" {
		return nil, errors.New(reason)
	}

	originalColors, err := k.getColors(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	dump, err := k.dumpTheme(ctx, theme)
	if err != nil {
		return nil, err
	}
	if err := k.setColorsFromContent(ctx, endpoint, dump); err != nil {
		return nil, err
	}

	return func(ctx context.Context) error {
		return k.setColorsFromContent(ctx, endpoint, originalColors)
	}, nil
}

func (k *Kitty) Apply(ctx context.Context, theme string) error {
	result, err := k.runner.Run(ctx, "kitty", "+kitten", "themes", "--reload-in=all", theme)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return commandResultError(result)
	}
	return nil
}

func (k *Kitty) dumpTheme(ctx context.Context, theme string) (string, error) {
	result, err := k.runner.Run(ctx, "kitty", "+kitten", "themes", "--dump-theme", theme)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", commandResultError(result)
	}
	return result.Stdout, nil
}

func (k *Kitty) previewEndpoint(ctx context.Context) (kittyRemoteEndpoint, string) {
	hints := k.readConfigHints()
	match := currentKittyWindowMatch()
	var failures []string

	for _, candidate := range k.remoteSocketCandidates(hints) {
		if reason := validateKittySocket(candidate.socket); reason != "" {
			failures = append(failures, reason)
			continue
		}
		endpoint := kittyRemoteEndpoint{
			socket: candidate.socket,
			match:  match,
			reason: candidate.label,
		}
		if err := k.probeRemote(ctx, endpoint); err == nil {
			return endpoint, ""
		} else {
			failures = append(failures, fmt.Sprintf("%s probe failed: %s", candidate.label, err.Error()))
		}
	}

	if strings.EqualFold(hints.allowRemoteControl, "socket-only") {
		return kittyRemoteEndpoint{}, kittyPreviewUnavailableReason(hints, failures)
	}

	if kittySessionAvailable() {
		endpoint := kittyRemoteEndpoint{
			match:  match,
			reason: "kitty remote control available in the current window",
		}
		if err := k.probeRemote(ctx, endpoint); err == nil {
			return endpoint, ""
		} else {
			failures = append(failures, "kitty tty remote control probe failed: "+err.Error())
		}
	}

	return kittyRemoteEndpoint{}, kittyPreviewUnavailableReason(hints, failures)
}

func (k *Kitty) probeRemote(ctx context.Context, endpoint kittyRemoteEndpoint) error {
	probeCtx, cancel := withKittyTimeout(ctx, kittyRemoteProbeTimeout)
	defer cancel()
	_, err := k.getColors(probeCtx, endpoint)
	return err
}

func (k *Kitty) getColors(ctx context.Context, endpoint kittyRemoteEndpoint) (string, error) {
	result, err := k.remote(ctx, endpoint, "get-colors")
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", commandResultError(result)
	}
	return result.Stdout, nil
}

func (k *Kitty) setColorsFromContent(ctx context.Context, endpoint kittyRemoteEndpoint, content string) error {
	file, err := os.CreateTemp("", "themer-kitty-*.conf")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	result, err := k.remote(ctx, endpoint, "set-colors", path)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return commandResultError(result)
	}
	return nil
}

func (k *Kitty) remote(ctx context.Context, endpoint kittyRemoteEndpoint, command string, args ...string) (CommandResult, error) {
	base := []string{"@"}
	if endpoint.socket != "" {
		base = append(base, "--to", endpoint.socket)
	}
	base = append(base, command)
	if endpoint.match != "" {
		base = append(base, "--match", endpoint.match)
	}
	base = append(base, args...)
	return k.runner.Run(ctx, "kitty", base...)
}

func (k *Kitty) remoteSocketCandidates(hints kittyConfigHints) []kittySocketCandidate {
	var candidates []kittySocketCandidate
	seen := map[string]struct{}{}
	add := func(socket, label string) {
		socket = normalizeKittySocket(socket)
		if socket == "" || socket == "none" {
			return
		}
		if _, ok := seen[socket]; ok {
			return
		}
		seen[socket] = struct{}{}
		candidates = append(candidates, kittySocketCandidate{
			socket: socket,
			label:  label,
		})
	}

	add(k.cfg.Socket, "configured kitty socket")
	add(os.Getenv("KITTY_LISTEN_ON"), "KITTY_LISTEN_ON socket")
	add(hints.listenOn, "kitty.conf listen_on socket")
	return candidates
}

func (k *Kitty) readConfigHints() kittyConfigHints {
	data, err := os.ReadFile(k.configPath)
	if err != nil {
		return kittyConfigHints{}
	}

	hints := kittyConfigHints{}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripKittyConfigComment(rawLine))
		if line == "" {
			continue
		}
		key, value, ok := kittyConfigDirective(line)
		if !ok {
			continue
		}
		switch key {
		case "allow_remote_control":
			hints.allowRemoteControl = strings.TrimSpace(value)
		case "listen_on":
			hints.listenOn = normalizeKittySocket(value)
		}
	}
	return hints
}

func currentKittyWindowMatch() string {
	if windowID := strings.TrimSpace(os.Getenv("KITTY_WINDOW_ID")); windowID != "" {
		return "id:" + windowID
	}
	return ""
}

func kittySessionAvailable() bool {
	return strings.TrimSpace(os.Getenv("KITTY_PID")) != "" || strings.TrimSpace(os.Getenv("KITTY_WINDOW_ID")) != ""
}

func kittyPreviewUnavailableReason(hints kittyConfigHints, failures []string) string {
	if strings.EqualFold(hints.allowRemoteControl, "socket-only") {
		if hints.listenOn != "" {
			return fmt.Sprintf("kitty is configured for socket-only remote control but no live socket was found at %s; restart kitty or export KITTY_LISTEN_ON", displayKittySocket(hints.listenOn))
		}
		return "kitty is configured for socket-only remote control but no live socket is available"
	}
	if len(failures) > 0 {
		return failures[0]
	}
	if !kittySessionAvailable() {
		return "run inside kitty or configure adapters.kitty.socket"
	}
	return "kitty remote control is unavailable"
}

func validateKittySocket(socket string) string {
	switch {
	case socket == "":
		return ""
	case strings.HasPrefix(socket, "fd:"):
		return "kitty fd remote-control transports are not supported by themer"
	case strings.HasPrefix(socket, "unix:"):
		path := strings.TrimPrefix(socket, "unix:")
		if _, err := os.Stat(path); err != nil {
			return fmt.Sprintf("kitty socket %s is unavailable", path)
		}
		return ""
	default:
		return ""
	}
}

func normalizeKittySocket(socket string) string {
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return ""
	}
	if strings.HasPrefix(socket, "unix:") {
		path := strings.TrimPrefix(socket, "unix:")
		path = strings.ReplaceAll(path, "{kitty_pid}", strings.TrimSpace(os.Getenv("KITTY_PID")))
		path = expandHomePath(path)
		return "unix:" + path
	}
	return socket
}

func displayKittySocket(socket string) string {
	if strings.HasPrefix(socket, "unix:") {
		return strings.TrimPrefix(socket, "unix:")
	}
	return socket
}

func stripKittyConfigComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func kittyConfigDirective(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "", "", false
	}
	key := parts[0]
	value := strings.TrimSpace(line[len(key):])
	return key, value, true
}

func expandHomePath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func withKittyTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func commandResultError(result CommandResult) error {
	message := strings.TrimSpace(result.Stderr)
	if message == "" {
		message = "unknown error"
	}
	return errors.New(message)
}

func parseKittyColors(content string) map[string]string {
	colors := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := parts[0]
		if !strings.HasPrefix(key, "color") && key != "background" && key != "foreground" && key != "cursor" && key != "selection_background" && key != "selection_foreground" {
			continue
		}
		colors[key] = parts[1]
	}
	return colors
}
