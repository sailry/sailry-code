package codehost_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sailry/sailry-code/internal/assembly/codehost"
	nodetest "github.com/sailry/sailry-code/internal/testsupport/node"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestAssemblyOwnsDevelopmentModules(t *testing.T) {
	base := node.HarnessAssembly()
	development := map[string]bool{"git": false, "worktrees": false, "databases": false, "ssh": false, "terminal": false}
	for _, module := range base.Modules {
		if _, ok := development[module.ID()]; ok {
			t.Fatalf("Harness assembly includes Code module %q", module.ID())
		}
	}

	assembly := codehost.Assembly()
	if assembly.Terminal != nil || assembly.Databases != nil || assembly.SSH != nil || assembly.LegacyWorkspaceModules {
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
	var database, ssh, terminal bool
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
			if services.Bind == nil {
				t.Fatalf("workspace provider %q has no handler binding", id)
			}
			workspace.Git = workspace.Git || services.Git
			workspace.Worktrees = workspace.Worktrees || services.Worktrees
		}
		_, hasDatabase := module.(node.DatabaseServiceProvider)
		_, hasSSH := module.(node.SSHServiceProvider)
		_, hasTerminal := module.(node.TerminalServiceProvider)
		database, ssh, terminal = database || hasDatabase, ssh || hasSSH, terminal || hasTerminal
	}
	if !workspace.Git || !workspace.Worktrees || !database || !ssh || !terminal {
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

func TestCodeSystemPromptReachesModel(t *testing.T) {
	assembly := codehost.Assembly()
	productPrompt := strings.TrimSpace(assembly.SystemPrompt)
	if !strings.Contains(productPrompt, "Sailry Code") {
		t.Fatal("Code assembly is missing its product prompt")
	}
	script := &agentToolScript{}
	script.reset(func(int, map[string]string) [][3]string { return nil })
	owner := nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), script, node.Options{Assembly: assembly})
	t.Cleanup(func() {
		if err := owner.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	config := nodetest.SessionConfig(t, owner, 200000, protocol.PermissionAsk)
	session := nodetest.Do[protocol.Session](t, owner, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Config: &config}})
	nodetest.Turn(t, owner, session, "Explain a binary search.", nodetest.Wait, nil)
	script.mu.Lock()
	defer script.mu.Unlock()
	if len(script.systemPrompts) != 1 || strings.Count(script.systemPrompts[0], productPrompt) != 1 {
		t.Fatal("expected the product prompt once in the model's system message")
	}
}
