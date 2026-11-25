package worktreesdesktop

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
	fixtures "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestManagedForkReusesCreatedCheckoutAfterFailedFork(t *testing.T) {
	project := fixtures.ProjectID{1}
	source := protocol.Worktree{ID: protocol.WorktreeID{1}, Project: &project}
	target := protocol.Worktree{ID: protocol.WorktreeID{2}, Project: &project, Path: "/fixture/linked"}
	session := protocol.Session{ID: protocol.SessionID{1}, Revision: 7}
	forked := protocol.Session{ID: protocol.SessionID{2}}
	var sent []string
	var submitted func([]string, func(bool))
	var opened protocol.SessionID
	var locked, admitted, refreshed, reported bool
	var completed []bool
	started := false
	ui.NewTester(func(c *ui.Context) {
		if started {
			return
		}
		started = true
		workspace := native.WorkflowWorkspace{
			Worktree: source,
			Read: func(_ string, command protocol.Command, done func(*ui.Context, protocol.Output)) {
				if input, ok := command.Data.(protocol.CommandInspectGitPayload); command.Kind != "inspect_git" || !ok || input.Worktree != source.ID {
					t.Fatalf("workspace inspection = %+v", command)
				}
				done(c, protocol.Output{Data: protocol.GitStatus{Head: new("head"), IndexRevision: new("index")}})
			},
			Prompt: func(title string, labels, values []string, submit func([]string, func(bool)), toggles ...native.WorkspacePromptToggle) {
				if title != "location_fork" || !slices.Equal(labels, []string{"git_new_branch"}) || len(values) != 1 || !strings.HasPrefix(values[0], "sailry/task-") ||
					len(toggles) != 1 || toggles[0].Label != "location_copy_changes" || !*toggles[0].Value {
					t.Fatal("the managed fork prompt lost its defaults")
				}
				submitted = submit
			},
			LockPrompt:  func(value bool) { locked = value },
			Admit:       func(tree protocol.Worktree) { admitted = tree.ID == target.ID },
			UseLocation: func(protocol.Worktree) { t.Fatal("fork moved the original conversation") },
			OpenSession: func(id protocol.SessionID) { opened = id },
			Refresh:     func() { refreshed = true },
		}
		conversation := native.WorkflowConversation{
			Session: &session, OpenWorkspace: func() (native.WorkflowWorkspace, bool) { return workspace, true },
			Report: func(_ *ui.Context, err error, _, conflict, _ string) {
				reported = err != nil && conflict == "workspace_changed"
			},
			Execute: func(_ string, command protocol.Command, done func(*ui.Context, protocol.Output, error)) {
				sent = append(sent, command.Kind)
				switch command.Kind {
				case "create_managed_worktree":
					input := command.Data.(protocol.CommandCreateManagedWorktreePayload)
					if input.Project != project || input.Source != source.ID || input.ExpectedHead != "head" || input.ExpectedIndex != "index" || input.Branch != "feature" || !input.IncludeChanges {
						t.Fatalf("managed checkout = %+v", input)
					}
					done(c, protocol.Output{Data: target}, nil)
				case "fork_conversation_at":
					input := command.Data.(protocol.CommandForkConversationAtPayload)
					if input.Session != session.ID || input.Worktree != target.ID || input.ExpectedRevision != session.Revision {
						t.Fatalf("session fork = %+v", input)
					}
					if len(sent) == 2 {
						done(c, protocol.Output{}, protocol.Fault{Code: protocol.ErrorCodeRevisionConflict})
					} else {
						done(c, protocol.Output{Data: forked}, nil)
					}
				default:
					t.Fatalf("unexpected command %q", command.Kind)
				}
			},
		}
		openManaged(conversation, true)
		if submitted == nil {
			t.Fatal("no managed checkout prompt")
		}
		done := func(ok bool) { completed = append(completed, ok) }
		submitted([]string{"feature"}, done)
		if !locked || !admitted || !reported || refreshed || opened != (protocol.SessionID{}) {
			t.Fatalf("failed fork: requests %v, locked %t, admitted %t, reported %t, refreshed %t, opened %v", sent, locked, admitted, reported, refreshed, opened)
		}
		submitted([]string{"feature"}, done)
	}, 400, 300).Frame()
	if !slices.Equal(sent, []string{"create_managed_worktree", "fork_conversation_at", "fork_conversation_at"}) ||
		!slices.Equal(completed, []bool{false, true}) || !refreshed || opened != forked.ID {
		t.Fatalf("retry: requests %v, completion %v, refreshed %t, opened %v", sent, completed, refreshed, opened)
	}
}
