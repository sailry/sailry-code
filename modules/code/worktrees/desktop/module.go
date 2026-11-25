package worktreesdesktop

import (
	"embed"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

//go:embed locales/*.yml
var locales embed.FS

// Register contributes the project and conversation worktree workflows.
func Register(assembly *native.Assembly) {
	assembly.Locales = append(assembly.Locales, locales)
	assembly.PluginControlReplacements = append(assembly.PluginControlReplacements,
		native.PluginControlReplacement{Package: "worktrees", Slot: "context", ID: "location"},
		native.PluginControlReplacement{Package: "worktrees", Slot: "project_menu", ID: "manage"},
		native.PluginControlReplacement{Package: "worktrees", Slot: "composer", ID: "create"},
		native.PluginControlReplacement{Package: "worktrees", Slot: "composer", ID: "fork"},
	)
	assembly.ProjectWorkflows = append(assembly.ProjectWorkflows, func(host native.ProjectWorkflowHost) {
		host.RegisterMenu(projectMenu(host))
		host.RegisterLocations(readLocations)
		host.RegisterWorkspaceAction("worktree_create", CreateAt)
		host.RegisterWorkspaceAction("worktree_manage", func(_ *ui.Context, workspace native.WorkflowWorkspace, _, _ string) { Manage(workspace) })
		host.RegisterWorkspaceAction("worktree_fork_from", forkFromBranch)
		host.RegisterWorktreeActions(native.ConversationWorktreeActions{
			Create:        func(conversation native.WorkflowConversation) { openManaged(conversation, false) },
			ForkTurn:      func(conversation native.WorkflowConversation) { openManaged(conversation, true) },
			ForkSession:   forkSession,
			Select:        selectLocation,
			BranchOptions: forkBranchOptions,
		})
	})
}

func forkBranchOptions(c *ui.Context, menu *controls.Menu, branches []native.WorkspaceBranch, choose func(*ui.Context, string, string)) {
	if !slices.ContainsFunc(branches, func(branch native.WorkspaceBranch) bool { return validForkBase(branch.Name, branch.Commit) }) {
		return
	}
	menu.Submenu(i18n.T(c, "git_fork_from_branch"), func(menu *controls.Menu) {
		for _, branch := range branches {
			if validForkBase(branch.Name, branch.Commit) && menu.Item(branch.Name).Chosen() {
				choose(c, branch.Name, branch.Commit)
			}
		}
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
				Manage(workspace)
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
					if m.Item(checkout.Branch).Checked(checkout.Main).Disabled(project.Offline).Chosen() {
						if workspace, open := host.ProjectWorkspace(project); open {
							selectLocation(workspace, c, checkout.Path)
						}
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
