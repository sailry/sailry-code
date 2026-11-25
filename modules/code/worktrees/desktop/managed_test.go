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
			Worktree:          source,
			Current:           func() bool { return true },
			CanSelectLocation: func() bool { return true },
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
			LockPrompt:         func(value bool) { locked = value },
			SetPromptUncertain: func(bool) {},
			Admit:              func(tree protocol.Worktree) { admitted = tree.ID == target.ID },
			UseLocation:        func(protocol.Worktree) { t.Fatal("fork moved the original conversation") },
			OpenSession:        func(id protocol.SessionID) { opened = id },
			Refresh:            func() { refreshed = true },
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

func TestForkFromBranchRetainsUncertainRequests(t *testing.T) {
	const base = "0123456789abcdef0123456789abcdef01234567"
	project := protocol.ProjectID{1}
	source := protocol.Worktree{ID: protocol.WorktreeID{1}, Project: &project}
	target := protocol.Worktree{ID: protocol.WorktreeID{2}, Project: &project}
	session := protocol.Session{ID: protocol.SessionID{1}, Project: &project, Worktree: source.ID, Revision: 8}
	forked := protocol.Session{ID: protocol.SessionID{2}, Project: &project, Worktree: target.ID}
	managerFrame(t, func(c *ui.Context) {
		var submit func([]string, func(bool))
		var sent, effects []string
		var opened []protocol.SessionID
		var finished []bool
		var detail string
		locked := false
		requests := make(map[string]protocol.Command)
		workspace := native.WorkflowWorkspace{
			Worktree: source, Session: &session, Current: func() bool { return true }, CanSelectLocation: func() bool { return true },
			Prompt: func(title string, labels, values []string, action func([]string, func(bool)), toggles ...native.WorkspacePromptToggle) {
				if title != "location_fork" || !slices.Equal(labels, []string{"git_new_branch"}) || len(values) != 1 || !strings.HasPrefix(values[0], "sailry/task-") || len(toggles) != 0 {
					t.Fatalf("unexpected branch fork prompt %q %q %q %+v", title, labels, values, toggles)
				}
				submit = action
			},
			SetPromptDetail: func(_, branch string) { detail = branch }, SetPromptAction: func(string) {},
			SetPromptUncertain: func(value bool) { locked = value }, LockPrompt: func(value bool) { locked = value },
			Admit: func(value protocol.Worktree) {
				if value.ID != target.ID {
					t.Fatalf("admitted worktree %s, want %s", value.ID, target.ID)
				}
			},
			UseLocation: func(protocol.Worktree) { t.Fatal("fork moved the existing session") },
			OpenSession: func(id protocol.SessionID) { opened = append(opened, id) },
			Refresh:     func() {}, Report: func(*ui.Context, error, string, string, string) {},
			Execute: func(scope string, command protocol.Command, done func(*ui.Context, protocol.Output, error)) {
				sent = append(sent, command.Kind)
				var result any
				switch input := command.Data.(type) {
				case protocol.CommandCreateManagedWorktreePayload:
					if input.Base == nil || *input.Base != base || input.Branch != "forked-branch" || input.Source != source.ID || input.Project != project || input.IncludeChanges || input.ExpectedHead != "" || input.ExpectedIndex != "" {
						t.Fatalf("wrong base command: %+v", input)
					}
					result = target
				case protocol.CommandForkConversationAtPayload:
					if input.Session != session.ID || input.Worktree != target.ID || input.ExpectedRevision != session.Revision {
						t.Fatalf("wrong conversation fork: %+v", input)
					}
					result = forked
				default:
					t.Fatalf("unexpected command: %+v", command)
				}
				if stored, ok := requests[scope]; !ok {
					requests[scope] = command
					effects = append(effects, command.Kind)
					done(c, protocol.Output{}, protocol.Fault{Code: protocol.ErrorCodeOutcomeUnknown})
				} else {
					if stored.Kind != command.Kind {
						t.Fatalf("changed request scope %q: %s to %s", scope, stored.Kind, command.Kind)
					}
					done(c, protocol.Output{Data: result}, nil)
				}
			},
		}
		forkFromBranch(c, workspace, "release", base)
		if submit == nil || detail != "release" || len(sent) != 0 {
			t.Fatalf("the branch must be shown without writing: detail=%q requests=%q", detail, sent)
		}
		done := func(ok bool) { finished = append(finished, ok) }
		submit([]string{"forked-branch"}, done)
		if !locked {
			t.Fatal("an uncertain creation allowed changes to the request")
		}
		submit([]string{"ignored-while-locked"}, done)
		submit([]string{"ignored-while-locked"}, done)
		submit([]string{"ignored-after-completion"}, done)
		if !slices.Equal(sent, []string{"create_managed_worktree", "create_managed_worktree", "fork_conversation_at", "fork_conversation_at"}) ||
			!slices.Equal(effects, []string{"create_managed_worktree", "fork_conversation_at"}) || !slices.Equal(finished, []bool{false, false, true, false}) ||
			!slices.Equal(opened, []protocol.SessionID{forked.ID}) {
			t.Fatalf("confirmation replayed a side effect: requests=%q effects=%q finished=%v opened=%v", sent, effects, finished, opened)
		}
	})
}
