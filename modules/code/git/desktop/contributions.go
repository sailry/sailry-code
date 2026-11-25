package desktop

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
	kit "github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func projectMenu(c *ui.Context, menu *ui.Menu, project *native.Project, current func() bool) {
	pr := viewOf(project)
	if !pr.liveState().Known {
		pr.inspectGit()
	}
	if !pr.gitWritable() {
		return
	}
	if menu.Item(i18n.T(c, "git_manage_branches")).Chosen() {
		pr.openBranchPicker(c, current)
	}
}

func (pr *repository) checkoutControl(c *ui.Context, inMenu bool, context native.GitComposerContext) {
	if pr.liveState().Kind == string(protocol.RepositoryKindDirectory) {
		return
	}
	current := pr.Data.Repo.Branch
	label := context.Base
	if label == "" {
		label = current
	}
	if label == "" {
		label = i18n.T(c, "git_detached")
	}
	available := context.Draft || current != ""
	kit.MenuButton(c, "git-composer-branch/"+pr.host.Worktree.String(), func(button ui.Element) {
		native.ComposerContextButton(c, button.Disabled(!context.Enabled || !available), icons.Network, label, inMenu)
	}, func(menu *kit.Menu) {
		if !context.Draft {
			if current != "" && menu.Item(i18n.T(c, "git_copy_branch")).Icon(icons.Copy).Chosen() {
				c.WriteClipboard(current)
			}
			if context.ForkBase != nil && len(pr.Data.Repo.Branches) != 0 {
				menu.Submenu(i18n.T(c, "git_fork_from_branch"), func(m *kit.Menu) {
					for _, branch := range pr.Data.Repo.Branches {
						if usableBranch(branch) && m.Item(branch.Name).Chosen() {
							context.ForkBase(c, branch.Name, branch.Commit)
						}
					}
				})
			}
			return
		}
		count, remote := 0, false
		for _, branch := range pr.Data.Repo.Branches {
			if !usableBranch(branch) {
				continue
			}
			if branch.Remote {
				remote = true
				continue
			}
			if count >= 100 && branch.Name != current && branch.Name != label {
				continue
			}
			count++
			if menu.Item(branch.Name).Checked(branch.Name == label).Chosen() {
				pr.selectConversationBranch(context, branch)
			}
		}
		if context.SelectBase == nil {
			return
		}
		if remote {
			menu.Submenu(i18n.T(c, "git_remote_branches"), func(m *kit.Menu) {
				count := 0
				for _, branch := range pr.Data.Repo.Branches {
					if !branch.Remote || !usableBranch(branch) || count >= 100 && branch.Name != label {
						continue
					}
					count++
					if m.Item(branch.Name).Checked(branch.Name == label).Chosen() {
						pr.selectConversationBranch(context, branch)
					}
				}
			})
		}
		menu.Separator()
		if menu.Item(i18n.T(c, "git_new_branch")).Icon(icons.Plus).Chosen() {
			pr.startConversationBranch(context)
		}
	}, kit.MenuOptions{At: ui.AnchorTopStart, Self: ui.AnchorBottomStart})
}

func usableBranch(branch native.WorkspaceBranch) bool {
	return branch.Name != "" && len(branch.Name) <= 480 && !strings.ContainsFunc(branch.Name, unicode.IsControl)
}

func (pr *repository) selectConversationBranch(context native.GitComposerContext, branch native.WorkspaceBranch) {
	if !branch.Remote && context.SelectCheckout != nil && context.SelectCheckout(branch.Name) {
		return
	}
	if context.SelectBase != nil && branch.Commit != "" {
		context.SelectBase(branch.Name, "sailry/"+protocol.NewWorktreeID().String()[:8], branch.Commit)
	}
}

func (pr *repository) startConversationBranch(context native.GitComposerContext) {
	if pr.host.ShowOverlay == nil || pr.status.Head == nil {
		return
	}
	head := *pr.status.Head
	pr.showPickerOverlay(context.Current)
	pr.Prompt("git_new_branch", []string{"settings_name"}, []string{""}, func(values []string, done func(bool)) {
		branch := strings.TrimSpace(values[0])
		if branch == "" {
			done(false)
			return
		}
		done(context.SelectBase(branch, branch, head))
	})
}

func (pr *repository) openBranchPicker(c *ui.Context, current func() bool) {
	if pr.host.ShowOverlay == nil {
		return
	}
	pr.showPickerOverlay(current)
	menus := pr.menus(c)
	menus.choose("git_manage_branches", menus.management(), "git_manage_branches").run(c)
}

func (pr *repository) showPickerOverlay(current func() bool) {
	pr.host.ShowOverlay(func(c *ui.Context) bool {
		if current != nil && !current() {
			return false
		}
		pr.GitDialogs(c)
		pr.ActionDialog(c)
		return pr.view.picker.open || pr.view.confirm.open || pr.HasActionPrompt()
	}, func() { pr.view.picker, pr.view.confirm = picker{}, confirm{} })
}

var gitTranslations = i18n.New(Locales)

func (pr *repository) changeBadge(status protocol.GitStatus) *native.StatusBadge {
	if status.Kind == protocol.RepositoryKindDirectory {
		return nil
	}
	var added, removed uint64
	partial := status.Truncated || status.OmittedPaths != 0
	for _, entry := range status.Entries {
		added += uint64(entry.Diff.Additions)
		removed += uint64(entry.Diff.Deletions)
		partial = partial || entry.Diff.Truncated
	}
	positive, negative := strconv.FormatUint(added, 10), strconv.FormatUint(removed, 10)
	label := func(key string) native.StatusText {
		return native.StatusText{Label: gitTranslations.In("en", key), Locales: map[string]string{"zh-CN": gitTranslations.In("zh-CN", key)}}
	}
	badge := &native.StatusBadge{
		ID: "native:git:changes", Label: native.StatusText{Label: "Git"}, Icon: icons.Network,
		Segment: []native.StatusSegment{{Text: native.StatusText{Label: "+" + positive}, Tone: kit.Positive}, {Text: native.StatusText{Label: "-" + negative}, Tone: kit.Negative}},
		Details: []native.StatusDetail{{Label: label("git_badge_added"), Value: positive}, {Label: label("git_badge_removed"), Value: negative},
			{Label: label("files"), Value: strconv.Itoa(len(status.Entries))}},
		Activate: func() { pr.host.OpenDiff(allChanges) },
	}
	if partial {
		badge.Details = append(badge.Details, native.StatusDetail{Label: label("git_badge_partial")})
	}
	return badge
}

func summarizeChanges(status protocol.GitStatus) *native.WorkspaceChanges {
	if status.Kind == protocol.RepositoryKindDirectory {
		return nil
	}
	value := &native.WorkspaceChanges{Files: len(status.Entries)}
	entries := slices.Clone(status.Entries)
	slices.SortStableFunc(entries, func(a, b protocol.GitEntry) int {
		var am, bm uint64
		if a.ModifiedMs != nil {
			am = *a.ModifiedMs
		}
		if b.ModifiedMs != nil {
			bm = *b.ModifiedMs
		}
		return cmp.Or(cmp.Compare(bm, am), cmp.Compare(a.Path, b.Path))
	})
	for _, entry := range entries {
		value.Added += int(entry.Diff.Additions)
		value.Removed += int(entry.Diff.Deletions)
	}
	for _, entry := range entries[:min(len(entries), native.RecentChanges)] {
		status := workspaceChange(entry.Unstaged)
		if status == "" {
			status = workspaceChange(entry.Staged)
		}
		if entry.Untracked {
			status = "A"
		}
		value.Recent = append(value.Recent, native.WorkspaceChangedFile{Path: entry.Path, Status: status, Added: int(entry.Diff.Additions), Removed: int(entry.Diff.Deletions)})
	}
	return value
}
