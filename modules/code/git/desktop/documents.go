package desktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func workspaceGitDocumentFile(diff protocol.GitDiff) native.GitDocumentFile {
	return native.GitDocumentFile{
		FileChange: native.WorkspaceChange{Path: diff.Path, Added: int(diff.Additions), Removed: int(diff.Deletions), Hunks: native.ParseDiff(diff.Text)},
		Binary:     diff.Binary, Truncated: diff.Truncated, Size: diff.Size,
	}
}

func (p *repository) openGitCommit(id string) {
	key := "git-document/commit/" + id
	delete(p.failedReads, key)
	p.read(key, protocol.Command{Kind: "read_git_commit", Data: protocol.CommandReadGitCommitPayload{Worktree: p.host.Worktree, Commit: id}}, func(_ *ui.Context, out protocol.Output) {
		value, ok := protocol.DataAs[protocol.GitCommit](out.Data)
		if out.Kind != "git_commit" || !ok {
			return
		}
		files := make([]native.GitDocumentFile, 0, len(value.Files))
		for _, file := range value.Files {
			files = append(files, workspaceGitDocumentFile(file))
		}
		title := value.Entry.ID[:min(8, len(value.Entry.ID))]
		p.host.OpenDiff(p.Project.SetGitCommit(id, title, value.Entry.Message, files, value.Truncated))
	})
}

func (p *repository) openGitStash(id string) {
	key := "git-document/stash/" + id
	delete(p.failedReads, key)
	p.read(key, protocol.Command{Kind: "read_git_stash", Data: protocol.CommandReadGitStashPayload{Worktree: p.host.Worktree, Commit: id}}, func(c *ui.Context, out protocol.Output) {
		value, ok := protocol.DataAs[protocol.GitDiff](out.Data)
		if out.Kind != "git_diff" || !ok {
			return
		}
		p.host.OpenDiff(p.Project.SetGitDiff(id, i18n.T(c, "git_stash_menu"), workspaceGitDocumentFile(value)))
	})
}

func (p *repository) openGitOutput() {
	key := "git-document/output"
	delete(p.failedReads, key)
	p.read(key, protocol.Command{Kind: "read_git_output", Data: protocol.CommandReadGitOutputPayload{Worktree: p.host.Worktree}}, func(c *ui.Context, out protocol.Output) {
		value, ok := protocol.DataAs[protocol.OutputGitOutputPayload](out.Data)
		if out.Kind != "git_output" || !ok {
			return
		}
		p.host.OpenDiff(p.Project.SetGitText("output", i18n.T(c, "git_output"), value.Text))
	})
}
