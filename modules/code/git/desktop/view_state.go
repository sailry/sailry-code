package desktop

import (
	"github.com/egoist/mygo/ui"
	kit "github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/desktop/theme"
)

func (pr *repository) gitHasHead() bool {
	if pr.live() {
		return pr.liveState().Head
	}
	return len(pr.Data.Repo.Commits) > 0
}

// GitHistoryState is the history's read: Loading while a page is read, Next
// while the repository has commits after the ones listed, Partial when a
// commit or the references were cut short.
type GitHistoryState struct{ Loading, Next, Partial bool }

func (pr *repository) gitConflicted(path string) bool {
	return pr.live() && pr.liveState().Conflicts[path]
}
func (pr *repository) gitWritable() bool {
	return !pr.live() || pr.liveState().Known && pr.liveState().Kind != "directory"
}
func (pr *repository) gitCanCommit() bool {
	return pr.gitWritable() && len(pr.liveState().Conflicts) == 0 && !pr.gitBusy()
}

// gitBusy reports a Git action under way: the header shows the spinner and
// Commit waits.
func (pr *repository) gitBusy() bool {
	return pr.live() && pr.running > 0
}

// gitVacant reports a live folder whose details show no repository: one
// not read yet, or a plain directory (gitEmpty with details).
func (pr *repository) gitVacant() bool {
	return pr.live() && (!pr.liveState().Known || pr.liveState().Kind == "directory")
}

func (pr *repository) gitEmpty(c *ui.Context, details bool) bool {
	state := pr.liveState()
	if !pr.live() {
		return false
	}
	if !state.Known {
		ui.Column(c).Grow(1)
		return true
	}
	if state.Kind != "directory" && (state.Kind != "unborn" || details) {
		return false
	}
	key := "git_unborn_title"
	if state.Kind == "directory" {
		key = "git_directory_title"
	}
	if len(state.Repositories) > 0 {
		key = "git_repositories"
	}
	// The details column has the button; the main area only says so.
	var initialize func()
	if state.Kind == "directory" && details {
		initialize = func() {
			label := i18n.T(c, "git_initialize")
			kit.Primary(c.Key("initialize"), label, false).Children(func() { ui.Text(c, label).FontSize(14) }).OnClick(func() { pr.dispatch(c, native.GitAction{Kind: "git_initialize"}) })
		}
	}
	ui.Column(c.Key("git-empty")).Grow(1).Center().Padding(16).Gap(12).Children(func() {
		result := kit.ResultProps{Icon: icons.Network, Title: i18n.T(c, key, "count", len(state.Repositories))}
		if len(state.Repositories) == 0 {
			result.Extra = initialize
		}
		kit.Result(c, result)
		if len(state.Repositories) > 0 {
			ui.Column(c.Key("repositories")).FillWidth().MaxWidth(384).Border(1, theme.Of(c).Border).Radius(theme.Of(c).Radius).Children(func() {
				for index, name := range state.Repositories {
					ui.Row(c.Key(index)).Padding(8, 12).Gap(8).Children(func() { ui.Icon(c, icons.Folder).FontSize(14); ui.Text(c, name).FontSize(14).SingleLine() })
				}
			})
			if initialize != nil {
				initialize()
			}
		}
	})
	return true
}
