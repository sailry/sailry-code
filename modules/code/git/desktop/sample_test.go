package desktop

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
)

// The demo's Files and Git act on its workspace in memory: each action
// changes the index, the repository or the tree as its control says.
func TestSampleGitActions(t *testing.T) {
	open := func() (*repository, *native.WorkspaceData) {
		pr := viewOf(fixtures.PreviewProject(true))
		return pr, pr.Data
	}
	find := func(w *native.WorkspaceData, path string) *native.WorkspaceChange {
		for i := range w.Changes {
			if w.Changes[i].Path == path {
				return &w.Changes[i]
			}
		}
		return nil
	}
	git := func(t *testing.T, pr *repository, a GitAction) {
		t.Helper()
		if !pr.sampleGit(nil, a) {
			t.Fatalf("%s is not carried out", a.Kind)
		}
	}
	var url string
	submit := func(t *testing.T, pr *repository, value string, want bool) {
		t.Helper()
		if !pr.HasActionPrompt() {
			t.Fatal("no form opened")
		}
		window := ui.NewTesterWith(func(c *ui.Context) { pr.ActionDialog(c) }, 600, 400, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true, ReduceTransparency: true}})
		window.SetLocale("en")
		window.Frame()
		window.Frame()
		enter := func(label, text string) {
			field, ok := window.AccessByRole(ui.RoleTextField, en(label))
			if !ok {
				t.Fatalf("no field %q: %v", label, window.Texts())
			}
			if field.Value == text {
				return
			}
			window.ClickAt(field.Bounds.X+field.Bounds.W/2, field.Bounds.Y+field.Bounds.H/2)
			window.Frame()
			window.Key(ui.Cmd, ui.KeyA)
			window.Type(text)
			window.Frame()
		}
		enter("settings_name", value)
		if url != "" {
			enter("mcp_url", url)
		}
		if err := window.Click(en("settings_save")); err != nil {
			t.Fatal(err)
		}
		window.Frame()
		if pr.HasActionPrompt() == want {
			t.Fatalf("the form for %q stayed open: %v", value, pr.HasActionPrompt())
		}
	}

	t.Run("Index", func(t *testing.T) {
		pr, w := open()
		const added, edited = "internal/middleware/ratelimit.go", "internal/http/router.go"
		git(t, pr, GitAction{Kind: "git_stage", Paths: []string{added}})
		if e := find(w, added); e.Untracked || e.Staged != "A" || e.Unstaged != "" {
			t.Fatalf("Stage: %+v", e)
		}
		git(t, pr, GitAction{Kind: "git_unstage", Paths: []string{added}})
		if e := find(w, added); !e.Untracked || e.Staged != "" {
			t.Fatalf("Unstage: %+v", e)
		}
		git(t, pr, GitAction{Kind: "git_stage_all"})
		if slices.ContainsFunc(w.Changes, func(e native.WorkspaceChange) bool { return e.Unstaged != "" || e.Untracked || e.Staged == "" }) {
			t.Fatalf("Stage all: %+v", w.Changes)
		}
		git(t, pr, GitAction{Kind: "git_unstage_all"})
		if slices.ContainsFunc(w.Changes, func(e native.WorkspaceChange) bool { return e.Staged != "" }) {
			t.Fatalf("Unstage all: %+v", w.Changes)
		}
		n := len(w.Changes)
		git(t, pr, GitAction{Kind: "git_discard_changes", Paths: []string{edited}})
		if len(w.Changes) != n-1 || find(w, edited) != nil {
			t.Fatalf("Discard left %d changes", len(w.Changes))
		}
	})

	t.Run("Commit", func(t *testing.T) {
		pr, w := open()
		head, n := w.Repo.Commits[0], len(w.Repo.Commits)
		staged := 0
		for _, e := range w.Changes {
			if e.Staged != "" && e.Unstaged == "" {
				staged++
			}
		}
		changes := len(w.Changes)
		pr.view.commit = "Limit bookings per client"
		git(t, pr, GitAction{Kind: "git_commit", Message: pr.view.commit, Scope: "auto"})
		top := w.Repo.Commits[0]
		if len(w.Repo.Commits) != n+1 || top.Message != "Limit bookings per client" || !slices.Contains(top.Refs, "HEAD") ||
			!slices.Equal(top.Parents, []string{head.ID}) || slices.Contains(w.Repo.Commits[1].Refs, "HEAD") {
			t.Fatalf("Commit: %+v over %+v", top, w.Repo.Commits[1])
		}
		if staged == 0 || len(w.Changes) != changes-staged || pr.view.commit != "" {
			t.Fatalf("Commit left %d of %d changes (%d staged), message %q", len(w.Changes), changes, staged, pr.view.commit)
		}
		git(t, pr, GitAction{Kind: "git_commit", Message: "Limit bookings by plan", Amend: true})
		if len(w.Repo.Commits) != n+1 || w.Repo.Commits[0].Message != "Limit bookings by plan" {
			t.Fatalf("Amend: %+v", w.Repo.Commits[0])
		}
		git(t, pr, GitAction{Kind: "git_undo_commit"})
		if len(w.Repo.Commits) != n || w.Repo.Commits[0].ID != head.ID || !slices.Contains(w.Repo.Commits[0].Refs, "HEAD") {
			t.Fatalf("Undo commit: %+v", w.Repo.Commits[0])
		}
		git(t, pr, GitAction{Kind: "git_push"})
		if b := w.Repo.Branches[0]; !b.Current || b.Tracking != "" {
			t.Fatalf("Push: %+v", b)
		}
	})

	t.Run("Branches", func(t *testing.T) {
		pr, w := open()
		git(t, pr, GitAction{Kind: "branch_switch", Target: "main"})
		if w.Repo.Branch != "main" || w.Repo.Upstream != "origin/main" || !w.Repo.Branches[1].Current || w.Repo.Branches[0].Current {
			t.Fatalf("Switch: %q tracking %q, %+v", w.Repo.Branch, w.Repo.Upstream, w.Repo.Branches)
		}
		heads := 0
		for _, cm := range w.Repo.Commits {
			if slices.Contains(cm.Refs, "HEAD") {
				heads++
				if !slices.Contains(cm.Refs, "heads/main") {
					t.Fatalf("HEAD is at %+v", cm)
				}
			}
		}
		if heads != 1 {
			t.Fatalf("%d commits carry HEAD", heads)
		}
		n := len(w.Repo.Branches)
		git(t, pr, GitAction{Kind: "branch_delete", Target: "fix/reserve-race"})
		if len(w.Repo.Branches) != n-1 {
			t.Fatalf("Delete left %d branches", len(w.Repo.Branches))
		}
		git(t, pr, GitAction{Kind: "branch_delete", Target: "main"})
		if len(w.Repo.Branches) != n-1 {
			t.Fatal("the branch checked out was deleted")
		}
		git(t, pr, GitAction{Kind: "branch_create"})
		submit(t, pr, "main", false) // taken: the form stays
		submit(t, pr, "feature/quotas", true)
		last := w.Repo.Branches[len(w.Repo.Branches)-1]
		if last.Name != "feature/quotas" || !slices.Contains(w.Repo.Commits[0].Refs, "heads/feature/quotas") {
			t.Fatalf("New branch: %+v, refs %q", last, w.Repo.Commits[0].Refs)
		}
		git(t, pr, GitAction{Kind: "git_tag_delete", Target: "v2.3.1"})
		if slices.Contains(w.Repo.Tags, "v2.3.1") {
			t.Fatalf("Delete tag: %q", w.Repo.Tags)
		}
		git(t, pr, GitAction{Kind: "git_tag_create"})
		submit(t, pr, "v2.4.0", true)
		if !slices.Contains(w.Repo.Tags, "v2.4.0") {
			t.Fatalf("New tag: %q", w.Repo.Tags)
		}
	})

	t.Run("Stashes", func(t *testing.T) {
		pr, w := open()
		changes, stashes := len(w.Changes), len(w.Repo.Stashes)
		git(t, pr, GitAction{Kind: "git_stash_all"})
		if len(w.Changes) != 0 || len(w.Repo.Stashes) != stashes+1 {
			t.Fatalf("Stash all left %d changes and %d stashes", len(w.Changes), len(w.Repo.Stashes))
		}
		git(t, pr, GitAction{Kind: "git_stash_apply_latest"})
		if len(w.Changes) != changes || len(w.Repo.Stashes) != stashes+1 {
			t.Fatalf("Apply: %d changes, %d stashes", len(w.Changes), len(w.Repo.Stashes))
		}
		git(t, pr, GitAction{Kind: "git_discard_all"})
		git(t, pr, GitAction{Kind: "git_stash_pop", Target: w.Repo.Stashes[0]})
		if len(w.Changes) != changes || len(w.Repo.Stashes) != stashes {
			t.Fatalf("Pop: %d changes, %d stashes", len(w.Changes), len(w.Repo.Stashes))
		}
		git(t, pr, GitAction{Kind: "git_stash_drop_all"})
		if len(w.Repo.Stashes) != 0 {
			t.Fatalf("Drop all left %q", w.Repo.Stashes)
		}
	})

	t.Run("Remotes", func(t *testing.T) {
		pr, w := open()
		n := len(w.Repo.Remotes)
		git(t, pr, GitAction{Kind: "git_add_remote"})
		url = "https://git.harbor.example/mirror/harbor-api.git"
		submit(t, pr, w.Repo.Remotes[0].Name, false) // the name is taken: the form stays
		submit(t, pr, "mirror", true)
		if len(w.Repo.Remotes) != n+1 || w.Repo.Remotes[n].Name != "mirror" || w.Repo.Remotes[n].URL == "" {
			t.Fatalf("Add remote: %+v", w.Repo.Remotes)
		}
		git(t, pr, GitAction{Kind: "git_remove_remote", Remote: "mirror"})
		if len(w.Repo.Remotes) != n {
			t.Fatalf("Remove remote: %+v", w.Repo.Remotes)
		}
		// Ignore takes an untracked file out of the changes and leaves a
		// tracked one.
		const added, edited = "internal/middleware/ratelimit.go", "internal/http/router.go"
		changes := len(w.Changes)
		git(t, pr, GitAction{Kind: "git_ignore", Paths: []string{added, edited}})
		if len(w.Changes) != changes-1 || find(w, added) != nil || find(w, edited) == nil {
			t.Fatalf("Ignore left %d of %d changes", len(w.Changes), changes)
		}
	})

	// What the demo cannot carry out in memory is not in its menus; the
	// preview, which tells its notice, keeps every item.
	t.Run("Menus", func(t *testing.T) {
		pr, _ := open()
		var labels func(list []action, into map[string]bool)
		labels = func(list []action, into map[string]bool) {
			for _, a := range list {
				into[a.label] = true
				labels(a.sub, into)
			}
		}
		for _, demo := range []bool{true, false} {
			shown, branch := map[string]bool{}, map[string]bool{}
			tt := ui.NewTester(func(c *ui.Context) {
				fixtures.SetDemo(c, demo)
				pr.host.HasWorkspaceAction = func(string) bool { return !demo }
				g := pr.menus(c)
				labels(g.root(), shown)
				// A branch's own actions are the picker Manage branches opens.
				for _, b := range g.management() {
					if b.run == nil || b.group == g.t("git_branch_actions") {
						continue
					}
					b.run(c)
					labels(pr.view.picker.items, branch)
					pr.view.picker = picker{}
				}
			}, 400, 300)
			tt.SetLocale("en")
			tt.Frame()
			for _, key := range []string{"git_clone", "worktree_manage", "git_output", "git_stash_view_open"} {
				if shown[i18n.In("en", key)] == demo {
					t.Errorf("demo %v: the Git menu has %s: %v", demo, key, !demo)
				}
			}
			for _, key := range []string{"git_rebase", "worktree_create"} {
				if branch[i18n.In("en", key)] == demo {
					t.Errorf("demo %v: a branch's actions have %s: %v", demo, key, !demo)
				}
			}
			// Every action left in the demo's menu is one it carries out.
			for _, key := range []string{"git_add_remote", "git_fetch", "git_stash_all", "git_tag_create_open", "git_checkout_open"} {
				if !shown[i18n.In("en", key)] {
					t.Errorf("demo %v: the Git menu lacks %s", demo, key)
				}
			}
		}
	})
}
