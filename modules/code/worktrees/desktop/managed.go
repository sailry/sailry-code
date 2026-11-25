package worktreesdesktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

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
		status, ok := valueOf[protocol.GitStatus](out)
		if !ok {
			return
		}
		if status.Head == nil || status.IndexRevision == nil {
			report(conversation, c, protocol.Fault{Code: protocol.ErrorCodeConflict, Message: "managed worktree requires a committed repository"})
			return
		}
		promptManaged(conversation, workspace, status, fork)
	})
}

func promptManaged(conversation native.WorkflowConversation, workspace native.WorkflowWorkspace, status protocol.GitStatus, fork bool) {
	includeChanges := true
	branch := "sailry/task-" + protocol.NewWorktreeID().String()[:8]
	title := "location_create"
	if fork {
		title = "location_fork"
	}
	var target *protocol.Worktree
	finish := func(done func(bool)) {
		if conversation.Session == nil {
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
				report(conversation, c, err)
				done(false)
				return
			}
			value, ok := valueOf[protocol.Session](out)
			if !ok {
				done(false)
				return
			}
			if !fork {
				workspace.UseLocation(*target)
			}
			workspace.OpenSession(value.ID)
			workspace.Refresh()
			done(true)
			controls.Toast(c, controls.Done, i18n.T(c, "workspace_completed"))
		})
	}
	workspace.Prompt(title, []string{"git_new_branch"}, []string{branch}, func(values []string, done func(bool)) {
		if target != nil {
			finish(done)
			return
		}
		input := protocol.CommandCreateManagedWorktreePayload{
			Project: *workspace.Worktree.Project, Source: workspace.Worktree.ID, Branch: values[0],
			ExpectedHead: *status.Head, ExpectedIndex: *status.IndexRevision, IncludeChanges: includeChanges,
		}
		command := protocol.Command{Kind: "create_managed_worktree", Data: input}
		conversation.Execute("managed-worktree/"+workspace.Worktree.ID.String(), command, func(c *ui.Context, out protocol.Output, err error) {
			if err != nil {
				workspace.LockPrompt(workspace.ReportMutation(c, command.Kind, "worktree_create", err))
				done(false)
				return
			}
			value, ok := valueOf[protocol.Worktree](out)
			if !ok {
				done(false)
				return
			}
			target = &value
			workspace.Admit(value)
			workspace.LockPrompt(true)
			finish(done)
			if conversation.Session == nil {
				controls.Toast(c, controls.Done, i18n.T(c, "workspace_completed"))
			}
		})
	}, native.WorkspacePromptToggle{Label: "location_copy_changes", Value: &includeChanges})
}

func report(conversation native.WorkflowConversation, c *ui.Context, err error) {
	conversation.Report(c, err, "workspace_action_failed", "workspace_changed", "workspace_action_unknown")
}
