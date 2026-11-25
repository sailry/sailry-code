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
	if assembly.CodeIndex != nil || assembly.Terminal != nil || assembly.Databases != nil || assembly.SSH != nil || assembly.LegacyWorkspaceModules {
		t.Fatal("Code assembly should select modules without service overrides or legacy selection")
	}
	if assembly.BundledDefaults == nil || len(assembly.BundledDefaults) != 0 {
		t.Fatalf("Code must explicitly select no default plugins: %#v", assembly.BundledDefaults)
	}
	if len(assembly.Modules) != len(base.Modules)+len(development) {
		t.Fatalf("Code assembly has %d modules, want %d", len(assembly.Modules), len(base.Modules)+len(development))
	}
	seen := make(map[string]bool, len(assembly.Modules))
	var workspace node.WorkspaceServices
	var database, ssh, terminal, index bool
	for _, module := range assembly.Modules {
		id := module.ID()
		if seen[id] {
			t.Fatalf("Code assembly repeats module %q", id)
		}
		seen[id] = true
		if _, ok := development[id]; ok {
			development[id] = true
		}
		if provider, ok := module.(node.WorkspaceServiceProvider); ok {
			services := provider.WorkspaceServices()
			workspace.Git = workspace.Git || services.Git
			workspace.Worktrees = workspace.Worktrees || services.Worktrees
		}
		_, hasDatabase := module.(node.DatabaseServiceProvider)
		_, hasSSH := module.(node.SSHServiceProvider)
		_, hasTerminal := module.(node.TerminalServiceProvider)
		_, hasIndex := module.(node.CodeIndexServiceProvider)
		database, ssh, terminal, index = database || hasDatabase, ssh || hasSSH, terminal || hasTerminal, index || hasIndex
	}
	if !workspace.Git || !workspace.Worktrees || !database || !ssh || !terminal || !index {
		t.Fatal("Code modules did not provide all development services")
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
