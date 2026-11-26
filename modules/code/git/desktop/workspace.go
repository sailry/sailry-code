package desktop

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	kit "github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

const (
	gitIndex        = "index"
	gitCommit       = "commit"
	gitSwitch       = "switch"
	gitMerge        = "merge"
	gitDelete       = "delete"
	gitCreate       = "branch_create"
	gitRename       = "branch_rename"
	gitOperation    = "action"
	workspaceAction = "workspace"
)

type repository struct {
	*native.Project
	view   repositoryView
	host   native.GitWorkspaceHost
	status protocol.GitStatus

	badge                *native.StatusBadge
	branches             protocol.GitBranches
	logCursor            *protocol.GitLogCursor
	logCommits           map[string]struct{}
	logDirty             bool
	diffs                map[string]bool
	diffContents         map[string][]native.WorkspaceHunk
	manualDiffs          map[string]workspaceGitDiff
	loading, failedReads map[string]bool
	readContext          context.Context
	cancel               context.CancelFunc
	unwatch              func()
	gitLastRead          time.Time
	gitRefreshTimer      *time.Timer
	initialized, pending bool
	running              int
	mutations            map[string]*gitMutation
	readReported         map[string]string
}

func mount(project *native.Project, host native.GitWorkspaceHost) native.GitWorkspaceBinding {
	p := &repository{Project: project, view: newView(), host: host, loading: map[string]bool{}, failedReads: map[string]bool{}, diffs: map[string]bool{}}
	return native.GitWorkspaceBinding{
		State: p, Frame: p.frame, Inspect: p.inspectGit, Refresh: p.refreshExplicit, Release: p.release,
		Prune: p.prune, ResetChanges: func() { p.projectStatus(p.status) }, ReadDiff: p.readDiff,
		Initialize: p.initialize, HasPending: func() bool { return len(p.mutations) != 0 },
		Checkout: p.checkoutControl,

		Badge: func() *native.StatusBadge { return p.badge },
	}
}

func (p *repository) scope() context.Context {
	if p.readContext == nil {
		p.readContext, p.cancel = context.WithCancel(p.host.Context)
	}
	return p.readContext
}

func (p *repository) read(key string, command protocol.Command, done func(*ui.Context, protocol.Output), failed ...func(*ui.Context, error)) {
	if p.host.Read == nil || p.loading[key] || p.failedReads[key] {
		return
	}
	ctx := p.scope()
	p.loading[key] = true
	p.host.Read(ctx, command, func(c *ui.Context, out protocol.Output, err error) {
		if ctx.Err() != nil {
			return
		}
		delete(p.loading, key)
		if err != nil {
			p.failedReads[key] = true
			for _, run := range failed {
				run(c, err)
			}
		} else {
			done(c, out)
		}
		p.reportRead(c, key, command.Kind, err)
	})
}

func (p *repository) frame(invalidated bool) {
	if p.host.Observe == nil {
		return
	}
	ctx := p.scope()
	if p.unwatch == nil {
		p.unwatch, _ = p.host.Observe(ctx, func() { p.pending = true })
	}
	if !p.initialized {
		p.initialized = true
		p.pending = false
		p.readGit()
		return
	}
	p.pending = p.pending || invalidated
	if !p.pending || p.loading["git-status"] || p.loading["git-branches"] || p.loading["git-log"] {
		return
	}
	// Coalesce watch pulses without deferring an explicit refresh.
	remaining := 750*time.Millisecond - time.Since(p.gitLastRead)
	if remaining > 0 {
		if p.gitRefreshTimer == nil {
			var timer *time.Timer
			timer = time.AfterFunc(remaining, func() {
				p.host.Post(func(*ui.Context) {
					if ctx.Err() == nil && p.gitRefreshTimer == timer {
						p.gitRefreshTimer = nil
					}
				})
			})
			p.gitRefreshTimer = timer
		}
		return
	}
	p.stopTimer()
	p.pending = false
	p.readGit()
}

func (p *repository) stopTimer() {
	if p.gitRefreshTimer != nil {
		p.gitRefreshTimer.Stop()
		p.gitRefreshTimer = nil
	}
}

func (p *repository) release() {
	if p.cancel != nil {
		p.cancel()
		p.cancel, p.readContext = nil, nil
	}
	if p.unwatch != nil {
		p.unwatch()
		p.unwatch = nil
	}
	p.stopTimer()
	p.initialized, p.pending = false, false
	clear(p.loading)
	clear(p.failedReads)
	p.resetGitHistory()
	clear(p.view.expanded)
	clear(p.view.diffOpen)
	p.view.chord, p.view.second = false, false
	p.view.picker, p.view.confirm = picker{}, confirm{}
	clear(p.readReported)
	if p.host.PublishInspectionNotice != nil {
		p.host.PublishInspectionNotice("")
	}
	clear(p.diffs)
	clear(p.diffContents)
	clear(p.manualDiffs)
	if !p.host.TurnReview() {
		for i := range p.Data.Changes {
			p.Data.Changes[i].Hunks = nil
		}
	}
}

func (p *repository) prune() {
	var released map[string]bool
	for key := range p.diffContents {
		_, relative, _ := strings.Cut(key, "/")
		if p.Git.UsesDiff(relative) {
			continue
		}
		delete(p.diffs, key)
		delete(p.manualDiffs, key)
		delete(p.diffContents, key)
		delete(p.failedReads, "diff/"+key)
		if released == nil {
			released = make(map[string]bool)
		}
		released[relative] = true
	}
	if len(released) != 0 && !p.host.TurnReview() {
		for i := range p.Data.Changes {
			if released[p.Data.Changes[i].Path] {
				p.Data.Changes[i].Hunks = nil
			}
		}
	}
}

func (p *repository) initialize(done func(bool)) {
	p.mutateNamed(workspaceAction, p.gitCommand(p.status, protocol.GitAction{Kind: "initialize"}), func(c *ui.Context, out protocol.Output) {
		ok := out.Kind != ""
		if ok {
			if p.readContext != nil {
				p.readGit()
			}
			kit.Toast(c, kit.Done, i18n.T(c, "workspace_completed"))
		}
		done(ok)
	})
}

func (p *repository) workspaceError(c *ui.Context, code protocol.ErrorCode, message string) {
	p.host.Report(c, &protocol.Fault{Code: code, Message: message})
}

func (p *repository) hunk(c *ui.Context, relative string, index int, action string) {
	if action == "keep" {
		return
	}
	for _, change := range p.Data.Changes {
		if change.Path == relative && index >= 0 && index < len(change.Hunks) {
			displayed := change.Hunks[index]
			if p.host.ApplyCheckpointHunk(c, relative, displayed, action) {
				return
			}
			p.manualGitHunk(c, relative, displayed, action)
			return
		}
	}
	p.workspaceError(c, protocol.ErrorCodeRevisionConflict, "hunk source is not loaded")
}
