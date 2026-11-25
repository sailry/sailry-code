package codehost_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sailry/sailry-code/internal/testsupport/node"
	databasesnode "github.com/sailry/sailry-code/modules/code/databases/node"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	sshnode "github.com/sailry/sailry-code/modules/code/ssh/node"
	terminalnode "github.com/sailry/sailry-code/modules/code/terminal/node"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/process"
	nativeprotocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

type legacyWorkspaceModule struct{ native.Module }

func TestPublicNodeModuleSelection(t *testing.T) {
	for _, selection := range []struct {
		name            string
		module          native.Module
		service         string
		legacyWorkspace bool
	}{
		{name: "none"},
		{name: "ssh", module: sshnode.Module(), service: "ssh"},
		{name: "databases", module: databasesnode.Module(), service: "databases"},
		{name: "git", module: gitnode.Module(), service: "git"},
		{name: "worktrees", module: worktreesnode.Module(), service: "worktrees"},
		{name: "terminal", module: terminalnode.Module(), service: "terminal"},
		{name: "legacy-worktrees", module: legacyWorkspaceModule{worktreesnode.Module()}, legacyWorkspace: true},
	} {
		t.Run(selection.name, func(t *testing.T) {
			t.Parallel()
			assembly := native.Assembly{LegacyWorkspaceModules: selection.legacyWorkspace}
			if selection.module != nil {
				assembly.Modules = []native.Module{selection.module}
			}
			environment := process.NewEnvironment([]string{"PATH=/usr/bin:/bin"})
			owner, err := native.StartWithOptions(t.Context(), filepath.Join(t.TempDir(), "profile"), native.Options{
				ProductID: "ultra", Assembly: assembly, Environment: &environment, MCP: sdktest.MCPOptions(),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := owner.Stop(context.Background()); err != nil {
					t.Error(err)
				}
			})
			project := nodetest.Command(t, owner, protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
				Name: "Module selection", Path: t.TempDir(), Source: protocol.ProjectsSource{Kind: "local"}, Appearance: protocol.DefaultProjectsAppearance(),
			}}).Data.(protocol.Project)
			var worktree protocol.WorktreeID
			for _, tree := range nodetest.Snapshot(t, owner).Worktrees {
				if tree.Project != nil && *tree.Project == project.ID && tree.Main {
					worktree = tree.ID
					break
				}
			}
			if worktree == (protocol.WorktreeID{}) {
				t.Fatal("ordinary project has no main worktree")
			}
			workspace := map[string]any{"worktree": worktree}
			sshRequest := nativeprotocol.NewRequestID()
			for _, probe := range []struct {
				module, output string
				command        protocol.Command
			}{
				{"ssh", "ssh_cancelled", protocol.Command{Kind: "cancel_ssh", Data: nativeprotocol.CommandCancelSshPayload{Request: sshRequest}}},
				{"databases", "database_profiles", protocol.Command{Kind: "list_databases"}},
				{"git", "git_status", nodetest.DecodeCommand(t, "inspect_git", workspace)},
				{"worktrees", "git_worktrees", nodetest.DecodeCommand(t, "list_worktrees", workspace)},
				{"terminal", "terminal_settings", protocol.Command{Kind: "read_terminal_settings"}},
				{"", "", nodetest.DecodeCommand(t, "list_ssh_terminals", map[string]any{"profile": protocol.NewSshID()})},
			} {
				if probe.module == "" || selection.service != probe.module {
					admitted, err := owner.Dispatch(t.Context(), owner.ID(), protocol.NewRequest(owner.ID(), probe.command))
					var fault *protocol.Fault
					if !errors.As(err, &fault) || fault.Code != protocol.ErrorCodeNotConfigured || admitted.Completion != nil {
						t.Fatalf("%s was not rejected before admission: %v", probe.command.Kind, err)
					}
					continue
				}
				if got := nodetest.Command(t, owner, probe.command); got.Kind != probe.output {
					t.Fatalf("%s returned %q, want %q", probe.command.Kind, got.Kind, probe.output)
				}
			}
			stream, err := owner.Subscribe(t.Context(), owner.ID(), protocol.Topic{Kind: "repository", Data: worktree})
			if stream != nil {
				t.Cleanup(func() { _ = stream.Close() })
			}
			if selection.service == "git" {
				if err != nil || stream == nil {
					t.Fatalf("the explicit Git provider could not subscribe: %v", err)
				}
			} else if stream != nil || !nodetest.FaultIs(err, protocol.ErrorCodeNotConfigured) {
				t.Fatalf("repository subscription exposed without an explicit Git provider: %v", err)
			}
			if selection.service == "databases" {
				profile := nodetest.Command(t, owner, protocol.Command{Kind: "save_database", Data: protocol.CommandSaveDatabasePayload{
					Profile: protocol.DatabaseProfile{ID: protocol.NewDatabaseID(), Name: "SQLite fixture", Connection: protocol.DatabaseConnection{
						Kind: "sqlite", Data: protocol.DatabaseConnectionSqlitePayload{Path: filepath.Join(t.TempDir(), "fixture.db")},
					}},
				}}).Data.(protocol.DatabaseProfile)
				output := nodetest.Command(t, owner, protocol.Command{Kind: "query_database", Data: protocol.CommandQueryDatabasePayload{
					Profile: profile.ID, ExpectedRevision: profile.Revision, Sql: "CREATE TABLE fixture (value INTEGER)", RowLimit: 1, TimeoutMs: 1000,
				}})
				if outcome, ok := output.Data.(nativeprotocol.DatabaseOutcome); output.Kind != "database_outcome" || !ok || outcome.Kind != "query" {
					t.Fatalf("database module did not execute its SQLite query: %+v", output)
				}
			}
		})
	}
}
