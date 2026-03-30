package testutil

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func RunAsync[T any](fn func() T) <-chan T {
	ch := make(chan T, 1)
	go func() {
		ch <- fn()
	}()
	return ch
}

func MustReceive[T any](t testing.TB, label string, ch <-chan T, wait time.Duration, debug func() string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(wait):
		t.Fatalf("%s did not complete within %s\n\n%s", label, wait, renderDebug(debug))
		var zero T
		return zero
	}
}

func MustStayBlocked[T any](t testing.TB, label string, ch <-chan T, wait time.Duration, debug func() string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s completed unexpectedly\n\n%s", label, renderDebug(debug))
	case <-time.After(wait):
	}
}

func GoroutineDump() string {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return string(buf[:n])
}

func JoinDebug(sections ...string) string {
	nonEmpty := make([]string, 0, len(sections))
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section != "" {
			nonEmpty = append(nonEmpty, section)
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

func LabeledSection(label, body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	return fmt.Sprintf("%s:\n%s", label, body)
}

func renderDebug(debug func() string) string {
	if debug == nil {
		return ""
	}
	return strings.TrimSpace(debug())
}
