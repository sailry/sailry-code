package worktreesdesktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

// Register contributes the project and conversation worktree workflows.
func Register(assembly *native.Assembly) {
	assembly.PluginControlReplacements = append(assembly.PluginControlReplacements,
		native.PluginControlReplacement{Package: "worktrees", Slot: "project_menu", ID: "manage"},
		native.PluginControlReplacement{Package: "worktrees", Slot: "composer", ID: "create"},
		native.PluginControlReplacement{Package: "worktrees", Slot: "composer", ID: "fork"},
	)
	assembly.ProjectWorkflows = append(assembly.ProjectWorkflows, func(host native.ProjectWorkflowHost) {
		host.RegisterMenu(projectMenu(host))
		host.RegisterLocations(readLocations)
		host.RegisterWorktreeActions(native.ConversationWorktreeActions{
			Create:      func(conversation native.WorkflowConversation) { openManaged(conversation, false) },
			ForkTurn:    func(conversation native.WorkflowConversation) { openManaged(conversation, true) },
			ForkSession: forkSession,
		})
	})
}

func readLocations(project native.ProjectLocationReader) {
	project.Read("git-worktrees", protocol.Command{Kind: "list_worktrees", Data: protocol.CommandListWorktreesPayload{Worktree: project.Main}}, func(out protocol.Output) {
		if locations, ok := valueOf[protocol.GitWorktrees](out); ok {
			project.Publish(locations)
		}
	})
}

func projectMenu(host native.ProjectWorkflowHost) native.ProjectMenuAction {
	create := func(project *native.ProjectMenuProject) {
		if draft, ok := host.NewDraft(project); ok {
			openManaged(draft, false)
		}
	}
	manage := func(project *native.ProjectMenuProject) {
		if draft, ok := host.NewDraft(project); ok {
			if workspace, open := draft.OpenWorkspace(); open {
				workspace.ManageLocations()
			}
		}
	}
	return native.ProjectMenuAction{
		Order: 30,
		Context: func(c *ui.Context, m *ui.Menu, project *native.ProjectMenuProject) {
			m.Submenu(i18n.T(c, "worktree_manage"), func(m *ui.Menu) {
				if m.Item(i18n.T(c, "worktree_create")).Chosen() {
					create(project)
				}
				m.Separator()
				for _, checkout := range project.Worktrees {
					if m.Item(checkout.Branch).Checked(checkout.Main).Chosen() {
						host.OpenLocation(project, checkout.Path)
					}
				}
				if len(project.Worktrees) > 1 {
					m.Separator()
					if m.Item(i18n.T(c, "worktree_remove")).Chosen() {
						manage(project)
					}
				}
			})
		},
		BeforeCopy: func(c *ui.Context, m *native.ProjectOverflowMenu, project *native.ProjectMenuProject) {
			if m.Item(i18n.T(c, "worktree_create")).Icon(icons.Plus).Disabled(project.Offline).Chosen() {
				create(project)
			}
		},
		AfterCopy: func(c *ui.Context, m *native.ProjectOverflowMenu, project *native.ProjectMenuProject) {
			if m.Item(i18n.T(c, "worktree_remove")).Icon(icons.CircleX).Disabled(project.Offline || len(project.Worktrees) < 2).Chosen() {
				manage(project)
			}
		},
	}
}

func valueOf[T any](out protocol.Output) (T, bool) {
	if value, ok := out.Data.(T); ok {
		return value, true
	}
	if value, ok := out.Data.(*T); ok && value != nil {
		return *value, true
	}
	var zero T
	return zero, false
}
