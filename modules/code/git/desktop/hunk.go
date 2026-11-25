package desktop

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	kit "github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
	"lukechampine.com/blake3"
)

type workspaceGitDiff struct {
	diff                protocol.GitDiff
	index, head, branch *string
}

func workspaceManualHunkIndex(displayed native.WorkspaceHunk, patch string) int {
	wanted := native.DiffHunkFingerprint(displayed)
	matched := -1
	for index, hunk := range native.ParseDiff(patch) {
		if native.DiffHunkFingerprint(hunk) == wanted {
			if matched >= 0 {
				return -1
			}
			matched = index
		}
	}
	return matched
}
func (p *repository) manualGitHunk(c *ui.Context, relative string, displayed native.WorkspaceHunk, action string) {
	scope := p.Project.Git.Scope()
	if scope == "" {
		scope = "all"
	}
	key := scope + "/" + relative
	captured, exists := p.manualDiffs[key]
	selected := workspaceManualHunkIndex(displayed, captured.diff.Text)
	if !exists || !p.diffs[key] || selected < 0 || captured.diff.Binary || captured.diff.Truncated || captured.index == nil || scope == "staged" || action != "stage" && action != "reject" {
		p.workspaceError(c, protocol.ErrorCodeRevisionConflict, "complete working-tree hunk source is not loaded")
		return
	}
	readKey := "manual-hunk/source/" + relative
	delete(p.failedReads, readKey)
	p.read(readKey, protocol.Command{Kind: "read_file", Data: protocol.CommandReadFilePayload{Worktree: p.host.Worktree, Path: relative}}, func(c *ui.Context, out protocol.Output) {
		source, ok := protocol.DataAs[protocol.FileContent](out.Data)
		if !ok || source.Revision == nil || source.Truncated {
			p.workspaceError(c, protocol.ErrorCodeRevisionConflict, "complete working file is unavailable for hunk review")
			return
		}
		digest := blake3.Sum256([]byte(captured.diff.Text))
		input := protocol.CommandApplyGitHunkPayload{Worktree: p.host.Worktree, Path: relative, Scope: protocol.GitDiffScope(scope), Operation: protocol.GitHunkOperation(action), Hunk: uint32(selected), DiffRevision: fmt.Sprintf("%x", digest), ExpectedRevision: *source.Revision, ExpectedHead: captured.head, ExpectedBranch: captured.branch, ExpectedIndex: *captured.index}
		kind := gitIndex
		if action != "stage" {
			kind = gitOperation
		}
		p.mutateNamed(kind, protocol.Command{Kind: "apply_git_hunk", Data: input}, func(c *ui.Context, out protocol.Output) {
			if _, ok := protocol.DataAs[protocol.GitHunkApplied](out.Data); out.Kind == "git_hunk_applied" && ok {
				if p.readContext != nil {
					p.host.Refresh()
				}
				kit.Toast(c, kit.Done, i18n.T(c, "workspace_completed"))
			}
		})
	})
}
