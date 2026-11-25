package codehost_test

import (
	"testing"

	"github.com/sailry/sailry-code/internal/assembly/codehost"
	"github.com/sailry/sailry-harness/sdk/native/node"
)

func TestAssemblyOwnsDevelopmentModules(t *testing.T) {
	base := node.HarnessAssembly()
	if base.CodeIndex != nil {
		t.Fatal("Harness assembly includes the Code index service")
	}
	development := map[string]bool{"git": false, "worktrees": false, "code-index": false, "databases": false, "ssh": false, "terminal": false}
	for _, module := range base.Modules {
		if _, ok := development[module.ID()]; ok {
			t.Fatalf("Harness assembly includes Code module %q", module.ID())
		}
	}

	assembly := codehost.Assembly()
	if assembly.CodeIndex == nil || assembly.Databases == nil || assembly.SSH == nil || assembly.Terminal == nil {
		t.Fatal("Code assembly omitted a development service")
	}
	defaults := map[string]bool{}
	for _, item := range assembly.BundledDefaults {
		if !item.Enabled || defaults[item.Name] {
			t.Fatalf("unexpected default selection: %v", item)
		}
		defaults[item.Name] = true
	}
	for _, name := range []string{"brief", "claude-auth", "commands", "context7", "copilot-auth", "databases", "files", "git", "github", "goals", "office", "openai-auth", "provider-status", "reminders", "scheduled-tasks", "ssh", "statistics", "workbuddy", "worktrees"} {
		if !defaults[name] {
			t.Errorf("Code assembly omitted default plugin %q", name)
		}
	}
	if len(defaults) != 19 {
		t.Errorf("Code default plugins = %v", defaults)
	}
	if len(assembly.Modules) != len(base.Modules)+len(development) {
		t.Fatalf("Code assembly has %d modules, want %d", len(assembly.Modules), len(base.Modules)+len(development))
	}
	seen := make(map[string]bool, len(assembly.Modules))
	for _, module := range assembly.Modules {
		id := module.ID()
		if seen[id] {
			t.Fatalf("Code assembly repeats module %q", id)
		}
		seen[id] = true
		if _, ok := development[id]; ok {
			development[id] = true
		}
	}
	for id, present := range development {
		if !present {
			t.Errorf("Code assembly omitted module %q", id)
		}
	}
	for _, module := range base.Modules {
		if !seen[module.ID()] {
			t.Errorf("Code assembly omitted Harness module %q", module.ID())
		}
	}
}
