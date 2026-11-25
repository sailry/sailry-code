package desktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func (p *repository) readDiff(relative, scope string) {
	if relative == "" || relative == ":changes" {
		return
	}
	if p.host.TurnReview() {
		if p.Project.Git.UsesFileDiff(relative) {
			p.host.ReadFile(relative)
			p.host.ReadCheckpointHunks(relative)
		}
		return
	}
	key := scope + "/" + relative
	if p.diffs[key] {
		p.projectDiff(relative, scope, p.diffContents[key], p.manualDiffs[key].diff)
		if p.Project.Git.UsesFileDiff(relative) && !p.manualDiffs[key].diff.Binary {
			p.host.ReadFile(relative)
		}
		return
	}
	value := protocol.GitDiffScope(scope)
	if value == "" {
		value = protocol.GitDiffScopeAll
	}
	captured := workspaceGitDiff{index: p.status.IndexRevision, head: p.status.Head, branch: p.status.Branch}
	p.read("diff/"+key, protocol.Command{Kind: "read_git_diff", Data: protocol.CommandReadGitDiffPayload{Worktree: p.host.Worktree, Path: relative, Scope: value}}, func(c *ui.Context, out protocol.Output) {
		diff, ok := protocol.DataAs[protocol.GitDiff](out.Data)
		if !ok || p.host.TurnReview() || !p.Git.UsesDiff(relative) {
			return
		}
		p.diffs[key] = true
		if p.manualDiffs == nil {
			p.manualDiffs = map[string]workspaceGitDiff{}
		}
		captured.diff = diff
		p.manualDiffs[key] = captured
		if p.diffContents == nil {
			p.diffContents = map[string][]native.WorkspaceHunk{}
		}
		p.diffContents[key] = native.ParseDiff(diff.Text)
		p.projectDiff(relative, scope, p.diffContents[key], diff)
		// An individual text diff compares the captured file revision when
		// applying a hunk. The combined view and binary files need no body.
		if !diff.Binary && p.Project.Git.UsesFileDiff(relative) {
			p.host.ReadFile(relative)
		}
		p.host.ReadCheckpointHunks(relative)
	})
}
func (p *repository) projectDiff(relative, scope string, hunks []native.WorkspaceHunk, source protocol.GitDiff) {
	for i := range p.Data.Changes {
		if p.Data.Changes[i].Path == relative {
			p.Data.Changes[i].Hunks = hunks
			p.Data.Changes[i].Diff = native.WorkspaceDiffState{Scope: scope, Binary: source.Binary, Partial: source.Truncated, Size: source.Size}
			return
		}
	}
}
