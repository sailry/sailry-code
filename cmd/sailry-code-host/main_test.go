package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sailry/sailry-harness/sdk/host"
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
		{name: "serve", args: []string{"serve"}},
		{name: "explicit", args: []string{"--data-dir", override}},
		{name: "explicit empty", args: []string{"--data-dir", ""}},
		{name: "help", args: []string{"--help"}},
		{name: "link", args: []string{"link", "--data-dir", override}},
		{name: "submit", args: []string{"submit", "--prompt", "--data-dir"}},
		{name: "invalid", args: []string{"--unknown"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			calls := 0
			code, err := run(fixture.args, func(args []string, options host.Options) (int, error) {
				calls++
				if !slices.Equal(args, fixture.args) {
					t.Fatalf("Code rewrote SDK arguments: %q, want %q", args, fixture.args)
				}
				identity := options.Identity
				if identity.ID != "code" || identity.DefaultProfile != "~/.sailry-code" || identity.LegacyProfile != "~/.sailry" {
					t.Fatalf("host profile identity = %+v", identity)
				}
				if got, err := identity.DataDir(""); err != nil || got != filepath.Join(home, ".sailry-code") {
					t.Fatalf("host default directory = %q, %v", got, err)
				}
				if !options.Assembly.HasModule("git") {
					t.Fatal("Code Host lost its product assembly")
				}
				return 7, failure
			})
			if calls != 1 || code != 7 || !errors.Is(err, failure) {
				t.Fatalf("SDK startup calls = %d, exit = %d, error = %v", calls, code, err)
			}
		})
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("Code touched profiles before SDK option parsing: %v, %v", entries, err)
	}
}
