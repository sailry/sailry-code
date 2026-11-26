package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sailry/sailry-harness/sdk/desktop"
)

func TestRunPreservesProfileSelectionAndSDKArguments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	override := filepath.Join(home, "isolated-profile")
	failure := errors.New("startup refused")
	for _, fixture := range []struct {
		name string
		args []string
	}{
		{name: "default"},
		{name: "explicit", args: []string{"--data-dir", override}},
		{name: "explicit equals", args: []string{"--data-dir=" + override}},
		{name: "explicit empty", args: []string{"--data-dir="}},
		{name: "preview", args: []string{"--preview"}},
		{name: "acceptance", args: []string{"--acceptance-ui", "--acceptance-dir", override}},
		{name: "help", args: []string{"--help"}},
		{name: "invalid", args: []string{"--unknown"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			calls := 0
			err := run(fixture.args, func(args []string, options desktop.Options) error {
				calls++
				if !slices.Equal(args, fixture.args) {
					t.Fatalf("Code rewrote SDK arguments: %q, want %q", args, fixture.args)
				}
				identity := options.Identity
				if identity.ID != "code" || identity.DefaultProfile != "~/.sailry-code" || identity.LegacyProfile != "~/.sailry" {
					t.Fatalf("desktop profile identity = %+v", identity)
				}
				if got, err := identity.DataDir(""); err != nil || got != filepath.Join(home, ".sailry-code") {
					t.Fatalf("desktop default directory = %q, %v", got, err)
				}
				if options.NativeName != "Sailry Code" || options.NativeName == identity.Release.Package.Name {
					t.Fatalf("Code shares Harness preferences or its instance name: %q", options.NativeName)
				}
				if len(options.Assembly.ProjectWorkflows) == 0 || !options.Host.HasModule("git") {
					t.Fatal("Code desktop lost its product assembly")
				}
				return failure
			})
			if calls != 1 || !errors.Is(err, failure) {
				t.Fatalf("SDK startup calls = %d, error = %v", calls, err)
			}
		})
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("Code touched profiles before SDK option parsing: %v, %v", entries, err)
	}
}
