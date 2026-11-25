package desktop

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/desktop/theme"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func TestGitWorkspaceScopesReleaseAndIgnoreLateReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		type read struct {
			ctx     context.Context
			command protocol.Command
			done    func(*ui.Context, protocol.Output, error)
		}
		main, linked := protocol.WorktreeID{1}, protocol.WorktreeID{2}
		var reads []read
		observers := map[protocol.WorktreeID]func(){}
		posts := make(chan func(*ui.Context), 4)
		open := func(id protocol.WorktreeID) *repository {
			project := fixtures.NewProject(id.String(), &fixtures.Workspace{}, &fixtures.Actions{})
			return mount(project, native.GitWorkspaceHost{
				Context: t.Context(), Worktree: id,
				Read: func(ctx context.Context, command protocol.Command, done func(*ui.Context, protocol.Output, error)) {
					reads = append(reads, read{ctx, command, done})
				},
				Observe: func(ctx context.Context, changed func()) (func(), error) {
					if ctx.Err() != nil || observers[id] != nil {
						t.Fatalf("invalid observer for %s", id)
					}
					observers[id] = changed
					return func() { delete(observers, id) }, nil
				},
				Post:          func(fn func(*ui.Context)) { posts <- fn },
				PublishStatus: func(protocol.GitStatus, bool) {},
				TurnReview:    func() bool { return false },
			}).State.(*repository)
		}
		first, second := open(main), open(linked)
		commandWorktree := func(r read) protocol.WorktreeID {
			if r.command.Kind != "inspect_git" {
				t.Fatalf("unexpected request: %v", r.command)
			}
			return r.command.Data.(protocol.CommandInspectGitPayload).Worktree
		}
		status := func(path string) protocol.Output {
			return protocol.Output{Kind: "git_status", Data: protocol.GitStatus{Kind: protocol.RepositoryKindReady, Entries: []protocol.GitEntry{{Path: path, Untracked: true}}}}
		}
		for range 3 {
			before := len(reads)
			previousChanges := len(second.Data.Changes)
			first.frame(false)
			if len(reads) != before+1 || commandWorktree(reads[before]) != main || len(observers) != 1 {
				t.Fatalf("main scope mounted incorrectly: reads=%d, watchers=%v", len(reads)-before, len(observers))
			}
			old := reads[before]
			first.release()
			if old.ctx.Err() == nil || len(observers) != 0 {
				t.Fatal("main checkout still owns reads or observation")
			}
			second.frame(false)
			if len(reads) != before+2 || commandWorktree(reads[before+1]) != linked || len(observers) != 1 {
				t.Fatal("linked checkout read or observation was misrouted")
			}
			old.done(nil, status("retired.txt"), nil)
			if len(first.Data.Changes) != 0 || len(second.Data.Changes) != previousChanges {
				t.Fatal("late status restored a retired checkout")
			}
			linkedRead := reads[before+1]
			linkedRead.done(nil, status("linked.txt"), nil)
			if second.Git.Change() != "linked.txt" || len(first.Data.Changes) != 0 {
				t.Fatal("linked status did not stay scoped to the linked checkout")
			}
			for _, pending := range reads[before+2:] {
				pending.done(nil, protocol.Output{}, nil)
			}
			observers[linked]()
			second.frame(false)
			if second.gitRefreshTimer == nil {
				t.Fatal("the watch pulse did not hit its quiet refresh floor")
			}
			second.release()
			time.Sleep(time.Second)
			synctest.Wait()
			if len(posts) != 0 || len(observers) != 0 || second.gitRefreshTimer != nil || linkedRead.ctx.Err() == nil {
				t.Fatal("a repository observation, timer or read survived release")
			}
		}
	})
}

func TestGitIndexMutationAndBusyBelongToTheWorkbench(t *testing.T) {
	p := repositoryFor(t, &native.WorkspaceData{})
	id := protocol.WorktreeID{2}
	p.host.Worktree = id
	p.status = protocol.GitStatus{Kind: protocol.RepositoryKindReady, IndexRevision: new("index"), Head: new("head"), Entries: []protocol.GitEntry{{Path: "conflicted", Conflicted: true}, {Path: "untracked", Untracked: true}}}
	var request protocol.Command
	var complete func(*ui.Context, protocol.Output, error)
	p.host.Mutate = func(command protocol.Command, done func(*ui.Context, protocol.Output, error)) {
		request, complete = command, done
	}
	p.gitAction(nil, native.GitAction{Kind: "git_stage_all"})
	input, ok := protocol.DataAs[protocol.CommandUpdateGitIndexPayload](request.Data)
	if !ok || input.Worktree != id || input.ExpectedIndex != "index" || input.ExpectedHead == nil || *input.ExpectedHead != "head" || len(input.Paths) != 1 || input.Paths[0] != "untracked" {
		t.Fatalf("stage all lost its checkout, index or conflict scope: %#v", request)
	}
	if p.running != 0 {
		t.Fatal("index staging changed the run-action spinner contract")
	}
	p.gitAction(nil, native.GitAction{Kind: "git_fetch"})
	if !p.gitBusy() || request.Kind != "run_git_action" {
		t.Fatal("admitted Git action did not own the busy state")
	}
	window := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, "", theme.Light)
		if complete != nil {
			done := complete
			complete = nil
			done(c, protocol.Output{Kind: "git_action"}, nil)
		}
	}, 640, 360)
	window.Frame()
	if p.gitBusy() || complete != nil {
		t.Fatal("settled Git action retained the busy state")
	}
}
