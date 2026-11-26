package desktop

import (
	"slices"

	"github.com/egoist/mygo/ui"
	kit "github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

const (
	barHeight = native.WorkspaceBarHeight
	rowHeight = native.WorkspaceRowHeight
	gitLeast  = 3*barHeight + 3*rowHeight
)

type repositoryView struct {
	state                      native.GitState
	sampleStashes              [][]native.WorkspaceChange
	sampleCommits              int
	nav                        changeNav
	expanded                   map[string]*bool
	current                    string
	view                       int
	commitID                   string
	diffOpen                   map[string]bool
	commit, draft              string
	tall, amend, signoff, skip bool
	after                      string
	chord, second              bool
	picker                     picker
	confirm                    confirm
	stashIDs                   []string
	tagIDs                     map[string]string
	operation                  bool
	log                        GitHistoryState
	historyCache               gitHistoryCache
	switchPending              []native.WorkspaceOperation
	list, history              ui.ScrollState
	side                       float32
}

func newView() repositoryView {
	return repositoryView{nav: defaultNav, expanded: map[string]*bool{}, after: "none", side: native.WorkspaceSideWidth}
}

func viewOf(project *native.Project) *repository {
	if owner, ok := project.GitWorkspace().State.(*repository); ok {
		return owner
	}
	// Preview projects have no live service, but their two surfaces still
	// share one isolated workbench.
	binding := mount(project, native.GitWorkspaceHost{})
	project.SetGitWorkspace(binding)
	return binding.State.(*repository)
}

func (pr *repository) live() bool { return pr.Project.Actions != nil }
func (pr *repository) liveState() native.GitState {
	if pr.host.Read != nil {
		return pr.view.state
	}
	return pr.Project.GitStatus()
}
func (pr *repository) hasAction(id string) bool {
	return pr.host.HasWorkspaceAction != nil && pr.host.HasWorkspaceAction(id)
}
func (pr *repository) dispatch(c *ui.Context, action native.GitAction) {
	if pr.host.Mutate != nil {
		action.Paths = slices.Clone(action.Paths)
		pr.gitAction(c, action)
		return
	}
	if pr.live() && pr.Project.Actions.Git != nil {
		pr.Project.Actions.Git(c, action)
		return
	}
	sampled(c, func() bool { return !pr.live() && pr.sampleGit(c, action) })
}

func (pr *repository) commitSaved(message string) {
	v := &pr.view
	if v.commit == message {
		v.commit = ""
	}
	v.amend, v.signoff, v.skip, v.tall = false, false, false, false
	v.after, v.draft = "none", ""
}

func (pr *repository) choose(title string, choices []native.GitChoice) {
	items := make([]action, 0, len(choices))
	for _, choice := range choices {
		items = append(items, action{label: choice.Label, detail: choice.Detail, run: choice.Run})
	}
	pr.view.picker = picker{open: true, title: title, items: items}
}

func (pr *repository) refreshControls(c *ui.Context) {
	if pr.host.Refresh != nil {
		pr.host.Refresh()
		return
	}
	sampled(c, func() bool { return !pr.live() })
}

func (pr *repository) readVisibleDiff(path, scope string) {
	if pr.host.Read != nil {
		pr.readDiff(path, scope)
		return
	}
	if pr.live() && pr.Project.Actions.ReadDiff != nil {
		pr.Project.Actions.ReadDiff(path, scope)
	}
}

func (pr *repository) renderDiff(c *ui.Context, path string, narrow bool) {
	if path == allChanges {
		pr.combined(c, narrow)
		return
	}
	if pr.RenderCapturedDiff(c, path, narrow) {
		return
	}
	if path != "" {
		pr.readVisibleDiff(path, pr.Git.Scope())
	}
	if pr.gitEmpty(c, false) {
		return
	}
	pr.RenderChangeDiff(c, path, narrow, func(c *ui.Context, change *native.WorkspaceChange, index int, operation string) {
		if pr.host.ApplyCheckpointHunk != nil {
			pr.hunk(c, change.Path, index, operation)
			return
		}
		if pr.live() && pr.Project.Actions.Hunk != nil {
			pr.Project.Actions.Hunk(c, change.Path, index, operation)
			return
		}
		if operation != "stage" {
			notice(c)
			return
		}
		sampled(c, func() bool {
			if pr.live() {
				return false
			}
			rest := change.Unstaged
			if len(change.Hunks) < 2 {
				rest = ""
			}
			stageChange(change)
			change.Unstaged = rest
			return true
		})
	})
}

func notice(c *ui.Context)                                   { kit.PreviewNotice(c) }
func scroll(state *ui.ScrollState, own bool) *ui.ScrollState { return native.OwnedScroll(state, own) }
