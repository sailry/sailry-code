package worktreesdesktop

import (
	"encoding/hex"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func forkFromBranch(c *ui.Context, workspace native.WorkflowWorkspace, branch, commit string) {
	session := workspace.Session
	project := workspace.Worktree.Project
	if session == nil || session.ID == (protocol.SessionID{}) || project == nil || session.Project == nil ||
		*session.Project != *project || session.Worktree != workspace.Worktree.ID {
		return
	}
	if !canSelectLocation(c, workspace) {
		return
	}
	if !validForkBase(branch, commit) {
		reportKey(workspace, c, "git_branch_revision", contextChanged)
		return
	}
	conversation := native.WorkflowConversation{Session: session, Execute: workspace.Execute, Report: workspace.Report}
	promptManaged(conversation, workspace, protocol.GitStatus{}, true, commit)
	workspace.SetPromptAction("location_fork")
	workspace.SetPromptDetail("worktree_fork_source", branch)
}

func validForkBase(branch, commit string) bool {
	if len(commit) != 40 || branch == "" || len(branch) > 480 || strings.ContainsFunc(branch, unicode.IsControl) {
		return false
	}
	var sha [20]byte
	_, err := hex.Decode(sha[:], []byte(commit))
	return err == nil
}

func forkSession(conversation native.WorkflowConversation) {
	session := conversation.Session
	if session == nil {
		return
	}
	command := protocol.Command{Kind: "read_conversation", Data: protocol.CommandReadConversationPayload{Session: session.ID, Limit: 100}}
	conversation.Execute("fork-context/"+session.ID.String(), command, func(c *ui.Context, out protocol.Output, err error) {
		if err != nil {
			report(conversation, c, err)
			return
		}
		history, ok := valueOf[protocol.ConversationHistory](out)
		if !ok {
			return
		}
		if !idle(history.Page) {
			report(conversation, c, protocol.Fault{Code: protocol.ErrorCodeBusy, Message: "conversation has active or queued turns"})
			return
		}
		openManaged(conversation, true)
	})
}

func idle(page protocol.ConversationPage) bool {
	if len(page.Queue.Items) != 0 {
		return false
	}
	for _, run := range page.Runs {
		switch run.Status {
		case protocol.ConversationStatusQueued, protocol.ConversationStatusRunning, protocol.ConversationStatusStopping:
			return false
		}
	}
	return true
}

func openManaged(conversation native.WorkflowConversation, fork bool) {
	if fork && conversation.Session == nil {
		return
	}
	workspace, ok := conversation.OpenWorkspace()
	if !ok || workspace.Worktree.Project == nil {
		return
	}
	workspace.Read("managed-worktree-status", protocol.Command{Kind: "inspect_git", Data: protocol.CommandInspectGitPayload{Worktree: workspace.Worktree.ID}}, func(c *ui.Context, out protocol.Output) {
		if conversation.Session != nil && !workspace.CanSelectLocation() {
			reportKey(workspace, c, "worktree_open_busy", nil)
			return
		}
		status, ok := valueOf[protocol.GitStatus](out)
		if !ok {
			return
		}
		if status.Head == nil || status.IndexRevision == nil {
			report(conversation, c, protocol.Fault{Code: protocol.ErrorCodeConflict, Message: "managed worktree requires a committed repository"})
			return
		}
		promptManaged(conversation, workspace, status, fork, "")
	})
}

func promptManaged(conversation native.WorkflowConversation, workspace native.WorkflowWorkspace, status protocol.GitStatus, fork bool, base string) {
	includeChanges := base == ""
	branch := "sailry/task-" + protocol.NewWorktreeID().String()[:8]
	title := "location_create"
	if fork {
		title = "location_fork"
	}
	var target *protocol.Worktree
	var createCommand *protocol.Command
	var uncertain bool
	complete := false
	finish := func(done func(bool)) {
		if conversation.Session == nil {
			complete = true
			workspace.UseLocation(*target)
			workspace.Refresh()
			done(true)
			return
		}
		session := conversation.Session
		command := protocol.Command{Kind: "move_conversation", Data: protocol.CommandMoveConversationPayload{Session: session.ID, Worktree: target.ID, ExpectedRevision: session.Revision}}
		if fork {
			command = protocol.Command{Kind: "fork_conversation_at", Data: protocol.CommandForkConversationAtPayload{Session: session.ID, Worktree: target.ID, ExpectedRevision: session.Revision}}
		}
		conversation.Execute("managed-conversation/"+session.ID.String(), command, func(c *ui.Context, out protocol.Output, err error) {
			if err != nil {
				if base != "" {
					uncertain = native.MutationRetained(err)
					if workspace.Current() {
						workspace.SetPromptUncertain(uncertain)
						workspace.LockPrompt(true)
					}
				}
				report(conversation, c, err)
				done(false)
				return
			}
			value, ok := valueOf[protocol.Session](out)
			if !ok || base != "" && (value.ID == session.ID || value.Worktree != target.ID || value.Project == nil || *value.Project != *workspace.Worktree.Project) {
				done(false)
				return
			}
			if !fork {
				workspace.UseLocation(*target)
			}
			if base == "" || workspace.Current() {
				workspace.OpenSession(value.ID)
			}
			complete = true
			workspace.Refresh()
			done(true)
			controls.Toast(c, controls.Done, i18n.T(c, "workspace_completed"))
		})
	}
	var toggles []native.WorkspacePromptToggle
	if base == "" {
		toggles = append(toggles, native.WorkspacePromptToggle{Label: "location_copy_changes", Value: &includeChanges})
	}
	workspace.Prompt(title, []string{"git_new_branch"}, []string{branch}, func(values []string, done func(bool)) {
		if complete || !uncertain && (!workspace.Current() || conversation.Session != nil && !workspace.CanSelectLocation()) {
			done(false)
			return
		}
		if target != nil {
			finish(done)
			return
		}
		branch := strings.TrimSpace(values[0])
		input := protocol.CommandCreateManagedWorktreePayload{Project: *workspace.Worktree.Project, Source: workspace.Worktree.ID, Branch: branch}
		if base == "" {
			input.ExpectedHead, input.ExpectedIndex, input.IncludeChanges = *status.Head, *status.IndexRevision, includeChanges
		} else {
			input.Base = &base
		}
		command := protocol.Command{Kind: "create_managed_worktree", Data: input}
		if createCommand != nil {
			command = *createCommand
		}
		scope := "managed-worktree/" + workspace.Worktree.ID.String()
		if base != "" {
			scope += "/" + conversation.Session.ID.String()
		}
		conversation.Execute(scope, command, func(c *ui.Context, out protocol.Output, err error) {
			uncertain = base != "" && native.MutationRetained(err)
			createCommand = nil
			if uncertain {
				createCommand = &command
			}
			if err != nil {
				if workspace.Current() {
					workspace.SetPromptUncertain(native.MutationRetained(err))
				}
				reportChange(workspace, c, command.Kind, err)
				done(false)
				return
			}
			value, ok := valueOf[protocol.Worktree](out)
			if !ok || base != "" && (value.ID == workspace.Worktree.ID || value.Project == nil || *value.Project != *workspace.Worktree.Project) {
				done(false)
				return
			}
			target = &value
			workspace.Admit(value)
			if !workspace.Current() {
				reportKey(workspace, c, "location_created_changed", contextChanged)
				done(false)
				return
			}
			workspace.SetPromptUncertain(false)
			workspace.LockPrompt(true)
			if conversation.Session != nil && !workspace.CanSelectLocation() {
				reportKey(workspace, c, "worktree_open_busy", nil)
				done(false)
				return
			}
			finish(done)
			if conversation.Session == nil {
				controls.Toast(c, controls.Done, i18n.T(c, "workspace_completed"))
			}
		})
	}, toggles...)
}

func report(conversation native.WorkflowConversation, c *ui.Context, err error) {
	conversation.Report(c, err, "workspace_action_failed", "workspace_changed", "workspace_action_unknown")
}
