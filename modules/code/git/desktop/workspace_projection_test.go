package desktop

import (
	"fmt"
	"testing"

	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func repositoryFor(t *testing.T, data *native.WorkspaceData) *repository {
	t.Helper()
	project := fixtures.NewProject("worktree", data, &fixtures.Actions{})
	return mount(project, native.GitWorkspaceHost{Context: t.Context(), TurnReview: func() bool { return false }, ReadFile: func(string) {}, ReadCheckpointHunks: func(string) {}}).State.(*repository)
}

func TestManualHunkSelection(t *testing.T) {
	patch := "@@ -1,2 +1,2 @@\n-before\n+after\n tail\n@@ -20,2 +20,2 @@\n-old\n+new\n end\n"
	hunks := native.ParseDiff(patch)
	if len(hunks) != 2 || workspaceManualHunkIndex(hunks[1], patch) != 1 {
		t.Fatal("manual selection lost captured patch position")
	}
	stale := hunks[1]
	stale.Lines = append([]native.WorkspaceDiffLine(nil), stale.Lines...)
	stale.Lines[1].Text = "unrelated"
	if workspaceManualHunkIndex(stale, patch) != -1 {
		t.Fatal("manual hunk accepted unrelated changed lines")
	}
}

func TestCapturedGitDocumentProjection(t *testing.T) {
	projected := workspaceGitDocumentFile(protocol.GitDiff{Path: "source.go", Text: "@@ -1 +1 @@\n-before Ω\n+after λ\n", Additions: 1, Deletions: 1, Binary: false, Truncated: true})
	if projected.Path != "source.go" || projected.Added != 1 || projected.Removed != 1 || projected.Binary || !projected.Truncated || len(projected.Hunks) != 1 || projected.Hunks[0].Lines[1].Text != "after λ" || projected.Hunks[0].Lines[1].New != 1 {
		t.Fatalf("document projection=%#v", projected)
	}
}

func TestTrackingSwitchScopes(t *testing.T) {
	branches := protocol.GitBranches{Sync: protocol.GitSync{Remotes: []string{"origin", "origin/team"}, Tracking: map[string]string{"local": "origin/topic"}}, Entries: []protocol.GitBranch{{Name: "local"}, {Name: "topic"}}}
	if local, collision := workspaceTrackingBranch(branches, "origin/topic"); local != "local" || collision {
		t.Fatalf("tracked local = %q conflict=%v", local, collision)
	}
	if local, collision := workspaceTrackingBranch(branches, "origin/team/topic"); local != "topic" || !collision {
		t.Fatalf("longest remote collision = %q conflict=%v", local, collision)
	}
	if local, collision := workspaceTrackingBranch(branches, "unknown/topic"); local != "" || collision {
		t.Fatal("unrecognized remote was converted to local branch")
	}
	// Pull from names the same remote, not the text before the first slash.
	if remote, branch, ok := workspaceRemoteBranch(branches.Sync.Remotes, "origin/team/topic"); remote != "origin/team" || branch != "topic" || !ok {
		t.Fatalf("pull target = %q %q %v", remote, branch, ok)
	}
	if _, _, ok := workspaceRemoteBranch(branches.Sync.Remotes, "unknown/topic"); ok {
		t.Fatal("unrecognized remote was accepted as a pull target")
	}
}

func TestGitHistoryPaging(t *testing.T) {
	data := &native.WorkspaceData{}
	cursor := &protocol.GitLogCursor{}
	p := repositoryFor(t, data)
	p.loading["git-log"] = true
	p.gitLogPages = []protocol.GitLog{
		{Next: cursor, Entries: []protocol.GitLogEntry{{ID: "head", Message: "latest", Timestamp: 1700000000, Parents: []string{"older"}}, {ID: "older", Message: "older"}}},
		{Offset: 100, ReferencesTruncated: true, Entries: []protocol.GitLogEntry{{ID: "older", Message: "older"}, {ID: "root", Message: "first", Parents: []string{}}}}}
	p.projectGitHistory(false)
	// A commit both pages hold is listed once. A root's parents are known
	// and none; an earlier Node's are not known.
	commits := data.Repo.Commits
	if len(commits) != 3 || commits[0].ID != "head" || commits[1].ID != "older" || commits[2].ID != "root" {
		t.Fatalf("history projection=%#v", commits)
	}
	if commits[0].Time.Unix() != 1700000000 || !commits[1].Time.IsZero() || commits[1].Parents != nil || commits[2].Parents == nil {
		t.Fatalf("history projection=%#v", commits)
	}
	// The last page read has no cursor: there is nothing more to read, and
	// a read under way is not started again.
	p.gitHistoryMore()
	if len(p.gitLogPages) != 2 {
		t.Fatal("a read started while one was under way")
	}
}

func TestGitCommitScope(t *testing.T) {
	kind := protocol.GitChangeKindModified
	status := protocol.GitStatus{Entries: []protocol.GitEntry{{Path: "a", Staged: &kind}, {Path: "b", Unstaged: &kind}}}
	if tracked, all := workspaceCommitScope(status, "auto"); tracked || all {
		t.Fatal("automatic commit included unstaged changes despite staged content")
	}
	if tracked, all := workspaceCommitScope(status, "all"); tracked || !all {
		t.Fatal("all commit excluded untracked content")
	}
	status.Entries[0].Staged = nil
	if tracked, all := workspaceCommitScope(status, "auto"); !tracked || all {
		t.Fatal("automatic commit did not use tracked changes")
	}
}

func TestChangesStatusSummary(t *testing.T) {
	status := protocol.GitStatus{Kind: protocol.RepositoryKindReady, Truncated: true}
	for index := range 12 {
		modified := uint64(index)
		status.Entries = append(status.Entries, protocol.GitEntry{Path: fmt.Sprintf("file-%02d", index), ModifiedMs: &modified, Diff: protocol.GitLineStats{Additions: uint64(index), Deletions: 1}})
	}
	projected := summarizeChanges(status)
	if projected.Files != 12 || projected.Added != 66 || projected.Removed != 12 || len(projected.Recent) != 10 {
		t.Fatalf("native badge = %#v", projected)
	}
	if projected.Recent[0].Path != "file-11" || projected.Recent[9].Path != "file-02" {
		t.Fatalf("recent order = %#v", projected.Recent)
	}
	status.Kind = protocol.RepositoryKindDirectory
	if summarizeChanges(status) != nil {
		t.Fatal("plain directory received a Git badge")
	}
}

func TestGitStatusProjection(t *testing.T) {
	data := &native.WorkspaceData{}
	p := repositoryFor(t, data)
	staged, unstaged := protocol.GitChangeKindAdded, protocol.GitChangeKindDeleted
	branch := "feature"
	p.projectStatus(protocol.GitStatus{Branch: &branch, Entries: []protocol.GitEntry{{Path: "added.go", Staged: &staged}, {Path: "deleted.go", Unstaged: &unstaged}, {Path: "new.go", Untracked: true}}})
	if data.Repo.Branch != "feature" || len(data.Changes) != 3 || data.Changes[0].Staged != "A" || data.Changes[1].Status != "D" || data.Changes[2].Status != "A" {
		t.Fatalf("status projection = %#v", data)
	}
	if p.Project.Git.Change() != "added.go" {
		t.Fatal("first real change did not become selected")
	}
	p.projectStatus(protocol.GitStatus{})
	if len(data.Changes) != 0 {
		t.Fatal("removed changes remained visible")
	}
}

func TestWorkspaceDiffMetadataFollowsCurrentProjection(t *testing.T) {
	const path = "fixture.bin"
	p := repositoryFor(t, &native.WorkspaceData{Changes: []native.WorkspaceChange{{Path: path}}})
	p.diffs = map[string]bool{"staged/" + path: true, "unstaged/" + path: true}
	p.manualDiffs = map[string]workspaceGitDiff{
		"staged/" + path:   {diff: protocol.GitDiff{Binary: true, Size: 100}},
		"unstaged/" + path: {diff: protocol.GitDiff{Truncated: true}},
	}
	p.readDiff(path, "staged")
	if got := p.Data.Changes[0].Diff; got != (native.WorkspaceDiffState{Scope: "staged", Binary: true, Size: 100}) {
		t.Fatalf("staged cache: %+v", got)
	}
	p.readDiff(path, "unstaged")
	if got := p.Data.Changes[0].Diff; got != (native.WorkspaceDiffState{Scope: "unstaged", Partial: true}) {
		t.Fatalf("unstaged cache: %+v", got)
	}
}
