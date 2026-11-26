package desktop

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

// A workspace refresh invalidates history. Only its visible panel requests it.
func (p *repository) resetGitHistory() {
	p.logCommits, p.logCursor, p.logDirty = nil, nil, false
	p.Data.Repo.Commits = nil
	p.view.log = GitHistoryState{}
	p.view.historyCache = gitHistoryCache{}
}

func (p *repository) gitHistoryMore() {
	if p.logCursor != nil {
		p.readGitLog(p.logCursor)
	}
}

func (p *repository) readGitLog(cursor *protocol.GitLogCursor) {
	if p.loading["git-log"] {
		return
	}
	if cursor == nil {
		p.logDirty = false
	}
	delete(p.failedReads, "git-log")
	p.view.log.Loading = true
	p.read("git-log", protocol.Command{Kind: "read_git_log", Data: protocol.CommandReadGitLogPayload{Worktree: p.host.Worktree, Limit: 100, Cursor: cursor}}, func(c *ui.Context, out protocol.Output) {
		p.view.log.Loading = false
		if page, ok := protocol.DataAs[protocol.GitLog](out.Data); ok && p.status.Kind != protocol.RepositoryKindDirectory {
			p.projectGitHistory(page, cursor == nil)
		}
	}, func(*ui.Context, error) { p.view.log.Loading = false })
}

// A page can repeat commits after the repository changes between reads.
func (p *repository) projectGitHistory(page protocol.GitLog, reset bool) {
	if reset {
		p.Data.Repo.Commits = nil
		p.logCommits = make(map[string]struct{}, len(page.Entries))
		p.view.log.Partial = false
	}
	p.view.historyCache = gitHistoryCache{}
	p.logCursor = page.Next
	p.view.log.Next = page.Next != nil
	p.view.log.Partial = p.view.log.Partial || page.ReferencesTruncated
	for _, entry := range page.Entries {
		p.view.log.Partial = p.view.log.Partial || entry.Truncated
		if _, seen := p.logCommits[entry.ID]; seen {
			continue
		}
		p.logCommits[entry.ID] = struct{}{}
		commit := native.WorkspaceCommit{ID: entry.ID, Message: entry.Message, Author: entry.Author, Refs: entry.References, Parents: entry.Parents}
		if entry.Timestamp != 0 {
			commit.Time = time.Unix(entry.Timestamp, 0)
		}
		p.Data.Repo.Commits = append(p.Data.Repo.Commits, commit)
	}
}
