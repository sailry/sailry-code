package desktop

import (
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func (p *repository) refreshExplicit() {
	clear(p.readReported)
	if p.host.PublishInspectionNotice != nil {
		p.host.PublishInspectionNotice("")
	}
	p.readGit()
}

func (p *repository) readGit() {
	p.gitLastRead = time.Now()
	if p.host.RefreshLocations != nil {
		p.host.RefreshLocations()
	}
	for key := range p.failedReads {
		if strings.HasPrefix(key, "git-") || strings.HasPrefix(key, "diff/") || strings.HasPrefix(key, "hunks/") {
			delete(p.failedReads, key)
		}
	}
	p.diffs = map[string]bool{}
	p.inspectGit()
}
func (p *repository) inspectGit() {
	p.read("git-status", protocol.Command{Kind: "inspect_git", Data: protocol.CommandInspectGitPayload{Worktree: p.host.Worktree}}, func(c *ui.Context, out protocol.Output) {
		if value, ok := protocol.DataAs[protocol.GitStatus](out.Data); ok {
			p.status = value
			p.badge = p.changeBadge(value)
			p.host.PublishStatus(value, true)
			p.projectStatus(value)
			if value.Kind != protocol.RepositoryKindDirectory {
				p.readGitHistory()
			} else {
				p.Data.Repo = native.WorkspaceRepository{}
			}
		}
	}, func(*ui.Context, error) {
		p.badge = nil
		p.host.PublishStatus(protocol.GitStatus{}, false)
		p.view.state = native.GitState{}
	})
}
func (p *repository) readGitHistory() {
	p.read("git-branches", protocol.Command{Kind: "list_git_branches", Data: protocol.CommandListGitBranchesPayload{Worktree: p.host.Worktree}}, func(c *ui.Context, out protocol.Output) {
		if value, ok := protocol.DataAs[protocol.GitBranches](out.Data); ok {
			p.branches = value
			p.projectBranches(c, value)
		}
	})
	p.readGitLog(nil, true)
}
func (p *repository) projectStatus(value protocol.GitStatus) {
	conflicts := map[string]bool{}
	for _, entry := range value.Entries {
		if entry.Conflicted {
			conflicts[entry.Path] = true
		}
	}
	p.view.state = native.GitState{Known: true, Kind: string(value.Kind), Repositories: append([]string(nil), value.Repositories...), Conflicts: conflicts, Head: value.Head != nil, HeadMessage: workspaceString(p.branches.Sync.HeadMessage)}
	if p.host.TurnReview() {
		return
	}
	old := map[string][]native.WorkspaceHunk{}
	for _, change := range p.Data.Changes {
		old[change.Path] = change.Hunks
	}
	p.Data.Changes = []native.WorkspaceChange{}
	for _, entry := range value.Entries {
		change := native.WorkspaceChange{Path: entry.Path, Staged: workspaceChange(entry.Staged), Unstaged: workspaceChange(entry.Unstaged), Untracked: entry.Untracked, Added: int(entry.Diff.Additions), Removed: int(entry.Diff.Deletions), Hunks: old[entry.Path]}
		if entry.Conflicted {
			// An unmerged path is a change still to be staged.
			change.Unstaged = "U"
		}
		change.Status = change.Unstaged
		if change.Status == "" {
			change.Status = change.Staged
		}
		if entry.Untracked {
			change.Status = "A"
		}
		p.Data.Changes = append(p.Data.Changes, change)
	}
	if value.Branch != nil {
		p.Data.Repo.Branch = *value.Branch
	} else {
		p.Data.Repo.Branch = ""
	}
	if p.Project.Git.Change() == "" && len(p.Data.Changes) > 0 {
		// The change a host shows first. Its diff opens with the host: a
		// status read opens no tab, and a closed diff stays closed.
		p.Project.Git.First(p.Data.Changes[0].Path)
	}
}
func workspaceChange(kind *protocol.GitChangeKind) string {
	if kind == nil {
		return ""
	}
	switch *kind {
	case protocol.GitChangeKindAdded:
		return "A"
	case protocol.GitChangeKindDeleted:
		return "D"
	case protocol.GitChangeKindRenamed:
		return "R"
	case protocol.GitChangeKindTypeChanged:
		return "T"
	}
	return "M"
}
func (p *repository) projectBranches(c *ui.Context, value protocol.GitBranches) {
	p.view.state.HeadMessage = workspaceString(value.Sync.HeadMessage)
	repo := &p.Data.Repo
	repo.Branches = nil
	repo.Remotes = nil
	repo.Tags = nil
	repo.Stashes = nil
	if value.Current != nil {
		repo.Branch = *value.Current
	} else {
		repo.Branch = ""
	}
	if value.Sync.Upstream != nil {
		repo.Upstream = *value.Sync.Upstream
	} else {
		repo.Upstream = ""
	}
	for _, branch := range value.Entries {
		repo.Branches = append(repo.Branches, native.WorkspaceBranch{Name: branch.Name, Commit: branch.Commit, Remote: branch.Remote, Current: branch.Current, Tracking: value.Sync.Tracking[branch.Name]})
	}
	names := []string{}
	for name := range value.Sync.RemoteUrls {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		repo.Remotes = append(repo.Remotes, native.WorkspaceRemote{Name: name, URL: value.Sync.RemoteUrls[name]})
	}
	for _, tag := range value.Sync.Tags {
		repo.Tags = append(repo.Tags, tag.Name)
	}
	for _, stash := range value.Sync.Stashes {
		repo.Stashes = append(repo.Stashes, stash.Message)
	}
	stashes := []string{}
	for _, stash := range value.Sync.Stashes {
		stashes = append(stashes, stash.Commit)
	}
	tags := map[string]string{}
	for _, tag := range value.Sync.Tags {
		tags[tag.Name] = tag.Commit
	}
	p.view.stashIDs, p.view.tagIDs = stashes, tags
	p.view.operation = value.Sync.Operation != nil
	if pk := &p.view.picker; pk.open && pk.key == "git_manage_branches" {
		pk.items = p.menus(c).management()
	}
}
