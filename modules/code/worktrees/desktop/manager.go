package worktreesdesktop

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

type worktreeEntry struct {
	tree                         protocol.GitWorktree
	label                        string
	marks                        []string
	current, stale, open, remove bool
}

func worktreeEntries(trees protocol.GitWorktrees, current string) []worktreeEntry {
	entries := make([]worktreeEntry, 0, len(trees.Entries))
	for _, tree := range trees.Entries {
		entry := worktreeEntry{tree: tree, label: worktreeLabel(tree), current: tree.Available && tree.Path == current}
		if tree.Main {
			entry.marks = append(entry.marks, "worktree_main")
		}
		if tree.Locked {
			entry.marks = append(entry.marks, "worktree_locked")
		}
		switch {
		case !tree.Available && !tree.Main && !tree.Locked:
			entry.stale = true
			entry.marks = append(entry.marks, "worktree_prune_on_select")
		case !tree.Available:
			entry.marks = append(entry.marks, "worktree_unavailable")
		default:
			entry.open = !entry.current
			entry.remove = !entry.current && !tree.Main && !tree.Locked && tree.Branch != nil && tree.Head != nil
		}
		entries = append(entries, entry)
	}
	return entries
}

func worktreeLabel(tree protocol.GitWorktree) string {
	if branch := text(tree.Branch); branch != "" {
		return branch
	}
	if head := text(tree.Head); head != "" {
		return head[:min(8, len(head))]
	}
	return worktreeName(tree.Path)
}

func worktreeName(path string) string {
	trimmed := strings.TrimRight(path, `/\`)
	if index := strings.LastIndexAny(trimmed, `/\`); index >= 0 {
		return trimmed[index+1:]
	}
	if trimmed == "" {
		return path
	}
	return trimmed
}

// Manage opens the checkout picker for the captured workspace.
func Manage(workspace native.WorkflowWorkspace) {
	command := protocol.Command{Kind: "list_worktrees", Data: protocol.CommandListWorktreesPayload{Worktree: workspace.Worktree.ID}}
	workspace.Execute("list-worktrees", command, func(c *ui.Context, out protocol.Output, err error) {
		if !workspace.Current() {
			return
		}
		trees, ok := valueOf[protocol.GitWorktrees](out)
		switch {
		case err != nil || !ok:
			reportKey(workspace, c, "worktree_load_failed", err)
		case workspace.Worktree.Project == nil:
			reportKey(workspace, c, "worktree_missing_project", nil)
		case trees.Kind == protocol.RepositoryKindDirectory:
			reportKey(workspace, c, "worktree_no_repository", nil)
		default:
			workspace.Pick(i18n.T(c, "worktree_manage"), worktreeRows(c, workspace, trees))
		}
	})
}

func worktreeRows(c *ui.Context, workspace native.WorkflowWorkspace, trees protocol.GitWorktrees) []native.WorkspacePickerRow {
	group := i18n.T(c, "workspace_worktrees")
	registered, sessions := workspace.Locations()
	unused := unusedWorktrees(registered, sessions)
	rows := make([]native.WorkspacePickerRow, 0, len(trees.Entries)+2)
	var base *protocol.GitWorktree
	for _, entry := range worktreeEntries(trees, workspace.Worktree.Path) {
		details := make([]string, 0, len(entry.marks)+2)
		for _, mark := range entry.marks {
			details = append(details, i18n.T(c, mark))
		}
		if unused[entry.tree.Path] && entry.tree.Available {
			details = append(details, i18n.T(c, "worktree_unused"))
		}
		details = append(details, worktreeName(entry.tree.Path))
		row := native.WorkspacePickerRow{Label: entry.label, Detail: strings.Join(details, " · "), Group: group, Icon: icons.GitBranch, Checked: entry.current}
		if entry.tree.Main {
			row.Icon = icons.Folder
		}
		switch {
		case entry.stale:
			row.Run = func(c *ui.Context) { confirmPrune(c, workspace, entry.tree) }
		case entry.tree.Available:
			row.Run = func(c *ui.Context) { workspace.Pick(entry.tree.Path, worktreeActions(c, workspace, entry)) }
		}
		rows = append(rows, row)
		if entry.tree.Available && entry.tree.Head != nil && (base == nil || entry.current) {
			base = &entry.tree
		}
	}
	if base != nil {
		branch, commit := text(base.Branch), text(base.Head)
		rows = append(rows, native.WorkspacePickerRow{Label: i18n.T(c, "worktree_create_and_use"), Group: i18n.T(c, "worktree_actions"), Icon: icons.Plus,
			Run: func(c *ui.Context) { createAt(c, workspace, branch, commit, true) }})
	}
	if trees.Truncated || trees.OmittedPaths > 0 {
		rows = append(rows, native.WorkspacePickerRow{Label: i18n.T(c, "worktree_listing_partial"), Disabled: true})
	}
	return rows
}

func unusedWorktrees(trees []protocol.Worktree, sessions []protocol.Session) map[string]bool {
	used := make(map[protocol.WorktreeID]bool, len(sessions))
	for _, session := range sessions {
		used[session.Worktree] = true
	}
	unused := map[string]bool{}
	for _, tree := range trees {
		if !tree.Main && !used[tree.ID] {
			unused[tree.Path] = true
		}
	}
	return unused
}

func worktreeActions(c *ui.Context, workspace native.WorkflowWorkspace, entry worktreeEntry) []native.WorkspacePickerRow {
	path := entry.tree.Path
	rows := make([]native.WorkspacePickerRow, 0, 3)
	if entry.open {
		rows = append(rows, native.WorkspacePickerRow{Label: i18n.T(c, "workspace_open_worktree"), Icon: icons.Folder,
			Run: func(c *ui.Context) { selectLocation(workspace, c, path) }})
	}
	rows = append(rows, native.WorkspacePickerRow{Label: i18n.T(c, "workspace_copy_path"), Icon: icons.Copy,
		Run: func(c *ui.Context) { c.WriteClipboard(path) }})
	if entry.remove {
		rows = append(rows, native.WorkspacePickerRow{Label: i18n.T(c, "worktree_remove"), Icon: icons.Trash,
			Run: func(c *ui.Context) { confirmRemoval(c, workspace, entry.tree) }})
	}
	return rows
}

func selectLocation(workspace native.WorkflowWorkspace, c *ui.Context, path string) {
	if !canSelectLocation(c, workspace) {
		return
	}
	withRegistration(workspace, c, path, "select", func(c *ui.Context, tree protocol.Worktree, err error) {
		if err != nil {
			reportChange(workspace, c, "register_worktree", err)
		} else if canSelectLocation(c, workspace) {
			workspace.SelectLocation(c, tree)
		}
	})
}

func canSelectLocation(c *ui.Context, workspace native.WorkflowWorkspace) bool {
	if !workspace.Current() {
		reportKey(workspace, c, "worktree_context_changed", contextChanged)
		return false
	}
	if !workspace.CanSelectLocation() {
		reportKey(workspace, c, "worktree_open_busy", nil)
		return false
	}
	return true
}

func withRegistration(workspace native.WorkflowWorkspace, c *ui.Context, path, scope string, done func(*ui.Context, protocol.Worktree, error)) {
	registered, _ := workspace.Locations()
	for _, tree := range registered {
		if tree.Path == path {
			done(c, tree, nil)
			return
		}
	}
	project := workspace.Worktree.Project
	if project == nil {
		done(c, protocol.Worktree{}, invalidWorktree)
		return
	}
	command := protocol.Command{Kind: "register_worktree", Data: protocol.CommandRegisterWorktreePayload{Project: *project, Path: path}}
	workspace.Execute(scope+"/register", command, func(c *ui.Context, out protocol.Output, err error) {
		tree, ok := valueOf[protocol.Worktree](out)
		if err == nil {
			if !ok || tree.Project == nil || *tree.Project != *project {
				err = invalidWorktree
			} else {
				workspace.Admit(tree)
			}
		}
		done(c, tree, err)
	})
}

func confirmRemoval(c *ui.Context, workspace native.WorkflowWorkspace, tree protocol.GitWorktree) {
	if !workspace.Current() {
		reportKey(workspace, c, "worktree_context_changed", contextChanged)
		return
	}
	var removing protocol.WorktreeID
	workspace.Confirm("worktree_remove", tree.Path, retryChange(workspace, "remove_worktree", func(run func(protocol.Command), done func(bool)) {
		withRegistration(workspace, c, tree.Path, "remove-worktree", func(c *ui.Context, registered protocol.Worktree, err error) {
			switch {
			case err != nil:
				if workspace.Current() {
					workspace.SetPromptUncertain(native.MutationRetained(err))
				}
				reportChange(workspace, c, "register_worktree", err)
			case !workspace.Current():
				reportKey(workspace, c, "worktree_context_changed", contextChanged)
			case workspace.HasDrafts(registered.ID):
				reportKey(workspace, c, "worktree_remove_unsaved", nil)
			default:
				workspace.SetPromptUncertain(false)
				removing = registered.ID
				run(protocol.Command{Kind: "remove_worktree", Data: protocol.CommandRemoveWorktreePayload{
					Worktree: registered.ID, ExpectedHead: text(tree.Head), ExpectedBranch: text(tree.Branch),
				}})
				return
			}
			done(false)
		})
	}, func(c *ui.Context, out protocol.Output) bool {
		removed, ok := valueOf[protocol.OutputWorktreeRemovedPayload](out)
		if !ok || removed.ID != removing {
			return false
		}
		workspace.Release(removed.ID)
		controls.Toast(c, controls.Done, i18n.T(c, "worktree_removed"))
		return true
	}), "worktree_remove_confirm")
}

func confirmPrune(c *ui.Context, workspace native.WorkflowWorkspace, tree protocol.GitWorktree) {
	if !workspace.Current() {
		reportKey(workspace, c, "worktree_context_changed", contextChanged)
		return
	}
	workspace.Confirm("worktree_prune", tree.Path, retryChange(workspace, "prune_worktree", func(run func(protocol.Command), done func(bool)) {
		inspect := protocol.Command{Kind: "inspect_git", Data: protocol.CommandInspectGitPayload{Worktree: workspace.Worktree.ID}}
		workspace.Execute("worktree-prune/inspect", inspect, func(c *ui.Context, out protocol.Output, err error) {
			status, ok := valueOf[protocol.GitStatus](out)
			if err != nil || !ok {
				reportKey(workspace, c, "worktree_prune_failed", err)
				done(false)
				return
			}
			if !workspace.Current() {
				done(false)
				return
			}
			run(protocol.Command{Kind: "run_git_action", Data: protocol.CommandRunGitActionPayload{
				Worktree: workspace.Worktree.ID, ExpectedHead: status.Head, ExpectedBranch: status.Branch, ExpectedIndex: text(status.IndexRevision),
				Action: protocol.GitAction{Kind: "prune_worktree", Data: protocol.GitActionPruneWorktreePayload{Path: tree.Path}},
			}})
		})
	}, func(c *ui.Context, _ protocol.Output) bool {
		workspace.RefreshLocations()
		controls.Toast(c, controls.Done, i18n.T(c, "worktree_pruned"))
		if workspace.Current() {
			Manage(workspace)
		}
		return true
	}), "worktree_prune_confirm")
}

// CreateAt creates a checkout without changing the current location.
func CreateAt(c *ui.Context, workspace native.WorkflowWorkspace, branch, commit string) {
	createAt(c, workspace, branch, commit, false)
}

func createAt(c *ui.Context, workspace native.WorkflowWorkspace, branch, commit string, use bool) {
	if !workspace.Current() {
		reportKey(workspace, c, "worktree_context_changed", contextChanged)
		return
	}
	if workspace.Worktree.Project == nil {
		reportKey(workspace, c, "worktree_missing_project", nil)
		return
	}
	title := "worktree_create"
	if use {
		if !canSelectLocation(c, workspace) {
			return
		}
		title = "worktree_create_and_use"
	}
	if branch == "" {
		branch = commit
	}
	var inputs []string
	submit := retryChange(workspace, "create_worktree", func(run func(protocol.Command), done func(bool)) {
		revision, branch, path := strings.TrimSpace(inputs[0]), strings.TrimSpace(inputs[1]), strings.TrimSpace(inputs[2])
		resolve := protocol.Command{Kind: "resolve_git_revision", Data: protocol.CommandResolveGitRevisionPayload{Worktree: workspace.Worktree.ID, Revision: revision}}
		workspace.Execute("create-worktree/resolve", resolve, func(c *ui.Context, out protocol.Output, err error) {
			resolved, ok := valueOf[protocol.OutputGitRevisionPayload](out)
			if err != nil || !ok {
				reportKey(workspace, c, "worktree_revision_failed", err)
				done(false)
				return
			}
			if !workspace.Current() || use && !canSelectLocation(c, workspace) {
				done(false)
				return
			}
			run(protocol.Command{Kind: "create_worktree", Data: protocol.CommandCreateWorktreePayload{
				Project: *workspace.Worktree.Project, Path: path, Branch: branch, Commit: resolved.Commit,
			}})
		})
	}, func(c *ui.Context, out protocol.Output) bool {
		tree, ok := valueOf[protocol.Worktree](out)
		if !ok || tree.Project == nil || *tree.Project != *workspace.Worktree.Project {
			return false
		}
		workspace.Admit(tree)
		controls.Toast(c, controls.Done, i18n.T(c, "worktree_created"))
		if use && canSelectLocation(c, workspace) {
			workspace.SelectLocation(c, tree)
		}
		return true
	})
	workspace.Prompt(title, []string{"worktree_base", "git_new_branch", "worktree_path"}, []string{branch, "", ""}, func(values []string, done func(bool)) {
		inputs = values
		submit(done)
	})
	workspace.SetPromptAction(title)
}

// Keep the exact input while asking about an uncertain operation; a revision or
// the Git index can change before the user checks its result.
func retryChange(workspace native.WorkflowWorkspace, action string, prepare func(func(protocol.Command), func(bool)), success func(*ui.Context, protocol.Output) bool) func(func(bool)) {
	var retained *protocol.Command
	return func(done func(bool)) {
		run := func(command protocol.Command) {
			workspace.Execute(action, command, func(c *ui.Context, out protocol.Output, err error) {
				uncertain := native.MutationRetained(err)
				if uncertain {
					retained = &command
				} else {
					retained = nil
				}
				if workspace.Current() {
					workspace.SetPromptUncertain(uncertain)
				}
				if err != nil || out.Kind == "" || !success(c, out) {
					reportChange(workspace, c, action, err)
					done(false)
					return
				}
				workspace.Refresh()
				done(true)
			})
		}
		if retained != nil {
			run(*retained)
		} else if workspace.Current() {
			prepare(run, done)
		} else {
			done(false)
		}
	}
}

func text(value *string) string {
	if value != nil {
		return *value
	}
	return ""
}
