// Package desktop drives a headless Desktop window over a real Node for
// module acceptance tests. Each test chooses the host and desktop modules it
// needs; this package selects none.
package desktop

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

// Width and Height are the window size every acceptance test lays out at.
const (
	Width  = 1280
	Height = 820
)

// Node is a test-owned Node in a temporary profile.
type Node struct {
	T       *testing.T
	Ctx     context.Context
	Profile string
	Owner   *node.Node
}

// StartNode starts a Node with the host modules the test selected.
func StartNode(t *testing.T, host node.Assembly) *Node {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	profile := filepath.Join(t.TempDir(), "profile")
	environment := process.NewEnvironment([]string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir(), "SHELL=/bin/sh"})
	owner, err := node.StartWithOptions(ctx, profile, node.Options{Assembly: host, Environment: &environment})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Stop(context.Background()) })
	return &Node{T: t, Ctx: ctx, Profile: profile, Owner: owner}
}

// Window is a headless Desktop window bound to a test-owned Node.
type Window struct {
	*fixtures.Window
	T      *testing.T
	Ctx    context.Context
	Owner  *node.Node
	Tester *ui.Tester
}

// Connect opens a window with the desktop modules the test selected, reading
// the profile's saved Desktop preferences as startup does.
func (n *Node) Connect(desktop native.Assembly) *Window {
	n.T.Helper()
	window, err := fixtures.Connect(n.Ctx, n.Owner, n.Profile, desktop)
	if err != nil {
		n.T.Fatal(err)
	}
	n.T.Cleanup(func() { _ = window.Close() })
	window.App.NavOpen = true
	tester := ui.NewTesterWith(window.App.View, Width, Height, ui.TesterOptions{Preferences: ui.Preferences{ReduceTransparency: true}})
	tester.SetLocale("en")
	tester.SetScale(0.125)
	return &Window{Window: window, T: n.T, Ctx: n.Ctx, Owner: n.Owner, Tester: tester}
}

// Open starts a Node and connects one window to it.
func Open(t *testing.T, host node.Assembly, desktop native.Assembly) *Window {
	t.Helper()
	return StartNode(t, host).Connect(desktop)
}

// Label is shared Desktop copy in English.
func Label(key string) string { return i18n.In("en", key) }

// Await draws frames until ready reports true, failing after ten seconds
// with the window's visible state.
func (w *Window) Await(message string, ready func() bool) {
	w.T.Helper()
	for end := time.Now().Add(10 * time.Second); ; time.Sleep(time.Millisecond) {
		w.Tester.Advance(17 * time.Millisecond)
		w.Tester.Frame()
		if ready() {
			return
		}
		if time.Now().After(end) || w.Ctx.Err() != nil {
			w.T.Fatalf("%s: page %q, selected %q, menu %q, texts %q", message, w.App.Page, w.App.Sidebar.Selected, w.Tester.Menu(), w.Tester.Texts())
		}
	}
}

// Do executes a command on the window's Node and fails on an error.
func (w *Window) Do(command protocol.Command) protocol.Output {
	w.T.Helper()
	output, err := w.Controller.Do(w.Ctx, command)
	if err != nil {
		w.T.Fatalf("%s: %v", command.Kind, err)
	}
	return output
}

// Click presses the center of an accessible element.
func (w *Window) Click(entry ui.AccessInfo) {
	b := entry.Bounds
	w.Tester.ClickAt(b.X+b.W/2, b.Y+b.H/2)
}

// Result reads an output's payload as T.
func Result[T any](t *testing.T, output protocol.Output) T {
	t.Helper()
	switch payload := output.Data.(type) {
	case T:
		return payload
	case *T:
		if payload != nil {
			return *payload
		}
	}
	t.Fatalf("unexpected %q payload %T, want %T", output.Kind, output.Data, new(T))
	return *new(T)
}

// String describes an accessible element for a failure message.
func String(entry ui.AccessInfo) string {
	return fmt.Sprintf("%q(role=%v bounds=%+v disabled=%t offscreen=%t)", entry.Name, entry.Role, entry.Bounds, entry.Disabled, entry.Offscreen)
}
