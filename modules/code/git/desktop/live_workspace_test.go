package desktop_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
	desktop "github.com/sailry/sailry-code/internal/testsupport/desktop"
	gitdesktop "github.com/sailry/sailry-code/modules/code/git/desktop"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	node "github.com/sailry/sailry-harness/sdk/native/node"
	nativeprotocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

const fixturePath = "fixture.txt"

func gitFixture(t *testing.T) (string, func(...string) string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		prefix := []string{"-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test"}
		output, err := exec.CommandContext(t.Context(), "git", append(prefix, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "--initial-branch=main")
	const path = fixturePath
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("old\n")
	git("add", "--", path)
	git("commit", "-m", "fixture")
	git("branch", "topic")
	write("new\n")

	return root, git
}

func TestGitLiveWorkspaceStagesAndReleases(t *testing.T) {
	root, git := gitFixture(t)
	w := desktop.Open(t, node.Assembly{Modules: []node.Module{gitnode.Module()}}, native.Assemble(gitdesktop.Register))
	const name = "Git test fixture"
	project := w.CreateProject(name, root)
	var logReads atomic.Int32
	w.Controller.OnDispatch(func(kind string) {
		if kind == "read_git_log" {
			logReads.Add(1)
		}
	})
	session := w.OpenProjectSession(project.ID)
	find := func(role ui.Role, label string) (ui.AccessInfo, bool) {
		for _, entry := range w.Tester.AccessAll() {
			if entry.Role == role && entry.Name == label && !entry.Offscreen && entry.Bounds.X >= native.RailWidth+w.App.NavWidth && entry.Bounds.W > 0 {
				return entry, true
			}
		}
		return ui.AccessInfo{}, false
	}
	w.Await("composer branch and Changes did not load", func() bool {
		_, branch := find(ui.RoleMenuButton, "main")
		_, badge := find(ui.RoleButton, "Git")
		return branch && badge
	})
	branch, _ := find(ui.RoleMenuButton, "main")
	w.Click(branch)
	w.Await("branch menu did not offer copying the bound branch", func() bool {
		_, ok := find(ui.RoleMenuItem, desktop.Label("git_copy_branch"))
		return ok || slices.Contains(w.Tester.Texts(), desktop.Label("git_copy_branch"))
	})
	if strings.Contains(strings.Join(w.Tester.Texts(), "|"), "topic") || git("branch", "--show-current") != "main" {
		t.Fatal("a saved conversation can switch its bound checkout")
	}
	w.Tester.Key(0, ui.KeyEscape)
	w.Tester.Frame()
	menu := w.ProjectMenu(name)
	label := desktop.Label("git_manage_branches")
	if got := countText(menu, label); got != 1 {
		t.Fatalf("want one project branch manager, got %d: %v", got, menu)
	}
	if err := w.Tester.ChooseMenuItem(label); err != nil {
		t.Fatal(err)
	}
	w.Await("project branch picker did not load", func() bool { return slices.Contains(w.Tester.Texts(), "topic") })
	w.Tester.Key(0, ui.KeyEscape)
	w.Tester.Frame()
	badge, _ := find(ui.RoleButton, "Git")
	w.Click(badge)
	w.Await("Changes did not open this conversation's Git diff", func() bool {
		return w.App.PanelOpen && w.App.Panel.Active.Kind == native.Changes && w.App.Panel.Active.ID == ":changes"
	})
	app, window := w.App, w.Tester
	app.Navigate(native.GitPage)
	app.SetResourceTarget(fixtures.ResourceTarget{Node: w.Owner.ID().String(), Project: project.ID.String(), Worktree: session.Worktree.String()})
	observed := func(repository bool) bool { _, ok := w.Controller.Workspace(session.Worktree, repository); return ok }
	w.Await("repository details or observation missing", func() bool {
		_, row := window.AccessByRole(ui.RoleTreeItem, fixturePath)
		return row && observed(true)
	})
	if observed(false) || app.NativePageEnabled(native.FilesPage) || slices.Contains(menu, desktop.Label("worktree_manage")) {
		t.Fatal("standalone Git activated Files or Worktrees")
	}
	row, _ := window.AccessByRole(ui.RoleTreeItem, fixturePath)
	window.RightClickAt(row.Bounds.X+row.Bounds.W/2, row.Bounds.Y+row.Bounds.H/2)
	if ctx := window.Menu(); slices.Contains(ctx, desktop.Label("git_open_source")) || !slices.Contains(ctx, desktop.Label("git_stage")) {
		t.Fatalf("unexpected change menu: %v", ctx)
	}
	if err := window.ChooseMenuItem(desktop.Label("git_stage")); err != nil {
		t.Fatal(err)
	}
	w.Await("staging the actual file", func() bool { return git("diff", "--cached", "--name-only") == fixturePath })
	if count := logReads.Load(); count != 0 {
		t.Fatalf("conversation and Changes requested Git history %d times", count)
	}
	if err := window.Click(desktop.Label("git_view_history")); err != nil {
		t.Fatal(err)
	}
	w.Await("History did not load the fixture commit", func() bool {
		_, shown := window.AccessByRole(ui.RoleOption, "fixture")
		return logReads.Load() != 0 && shown
	})
	if err := window.Click(desktop.Label("settings")); err != nil {
		t.Fatal(err)
	}
	w.Await("repository observation survived leaving its workspace", func() bool { return !observed(true) })
}

func TestGitLiveWorkspaceTargetsLinkedCheckout(t *testing.T) {
	root, git := gitFixture(t)
	linkedRoot := filepath.Join(filepath.Dir(root), "linked")
	git("worktree", "add", linkedRoot, "topic")
	const linkedFile = "linked-only.txt"
	if err := os.WriteFile(filepath.Join(linkedRoot, linkedFile), []byte("linked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := desktop.Open(t, node.Assembly{Modules: []node.Module{gitnode.Module(), worktreesnode.Module()}}, native.Assemble(gitdesktop.Register))
	project := w.CreateProject("Linked scope", root)
	linked := desktop.Result[protocol.Worktree](t, w.Do(protocol.Command{Kind: "register_worktree", Data: nativeprotocol.CommandRegisterWorktreePayload{Project: project.ID, Path: linkedRoot}}))
	main := w.OpenProjectSession(project.ID)
	observe := func(id protocol.WorktreeID) bool { _, ok := w.Controller.Workspace(id, true); return ok }
	w.Await("main checkout is not observed by the conversation", func() bool { return observe(main.Worktree) })
	w.App.Navigate(native.GitPage)
	w.App.SetResourceTarget(fixtures.ResourceTarget{Node: w.Owner.ID().String(), Project: project.ID.String(), Worktree: linked.ID.String()})
	w.Await("linked checkout did not replace the main workspace", func() bool {
		_, linkedRow := w.Tester.AccessByRole(ui.RoleTreeItem, linkedFile)
		_, mainRow := w.Tester.AccessByRole(ui.RoleTreeItem, fixturePath)
		return linkedRow && !mainRow && observe(linked.ID) && !observe(main.Worktree)
	})
	w.App.Navigate(native.ConversationPage)
	w.Await("linked checkout observation survived leaving its page", func() bool { return !observe(linked.ID) && observe(main.Worktree) })
}

func TestGitLegacyReleaseCoexistsAndDisables(t *testing.T) {
	path := os.Getenv("SAILRY_LEGACY_GIT_DIR")
	if path == "" {
		t.Skip("set SAILRY_LEGACY_GIT_DIR to the official Git 0.1.42 package directory")
	}
	for _, test := range []struct {
		name              string
		native, unrelated bool
	}{
		{"official only", false, false}, {"official and native", true, false}, {"unrelated same name and native", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			withNative := test.native
			packageDir, wantVersion := path, "0.1.42"
			if test.unrelated {
				packageDir, wantVersion = t.TempDir(), "0.1.420"
				if err := os.CopyFS(packageDir, os.DirFS(path)); err != nil {
					t.Fatal(err)
				}
				manifest := filepath.Join(packageDir, "plugin.json")
				body, err := os.ReadFile(manifest)
				if err != nil {
					t.Fatal(err)
				}
				var declaration map[string]json.RawMessage
				if err := json.Unmarshal(body, &declaration); err != nil {
					t.Fatal(err)
				}
				declaration["version"], _ = json.Marshal(wantVersion)
				body, err = json.Marshal(declaration)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(manifest, body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			root, _ := gitFixture(t)
			host := desktop.StartNode(t, node.Assembly{Modules: []node.Module{gitnode.Module()}})
			state := sdktest.Inspect(host.Owner)
			info, err := state.InstallPlugin(host.Ctx, packageDir, "", "git")
			version := ""
			if info.Summary.Version != nil {
				version = *info.Summary.Version
			}
			if err != nil || version != wantVersion || len(info.Issues) != 0 {
				t.Fatalf("Git fixture %q: version=%q, issues=%v, error=%v", wantVersion, version, info.Issues, err)
			}
			if err = state.Store().Node.Write(host.Ctx, func(db *sql.Conn) error {
				var err error
				info, err = sdktest.InstallPlugin(host.Ctx, db, info, 0, true)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			var assembly native.Assembly
			if withNative {
				assembly = native.Assemble(gitdesktop.Register)
			}
			w := host.Connect(assembly)
			project := w.CreateProject("Git fixture", root)
			session := w.OpenProjectSession(project.ID)
			count := func(label string) int {
				n := 0
				for _, entry := range w.Tester.AccessAll() {
					if entry.Name == label && (entry.Role == ui.RoleButton || entry.Role == ui.RoleMenuButton) && !entry.Offscreen && entry.Bounds.H > 0 {
						n++
					}
				}
				return n
			}
			w.Await("Git rail, badge and branch contributions did not resolve", func() bool {
				if test.unrelated {
					return count("Git") == 4 && count("main") == 2
				}
				return count("Git") == 2 && count("main") == 1
			})
			if withNative {
				menu := w.ProjectMenu(project.Name)
				if n := countText(menu, desktop.Label("git_manage_branches")); n != 1 {
					t.Fatalf("official package duplicated the native project menu: %v", menu)
				}
				w.Tester.Key(0, ui.KeyEscape)
			}
			w.Do(protocol.Command{Kind: "set_plugin_enabled", Data: protocol.CommandSetPluginEnabledPayload{Name: "git", ExpectedRevision: info.Summary.Revision, Enabled: false}})
			w.Await("disabling the legacy contribution affected the wrong owner", func() bool {
				if withNative {
					_, watched := w.Controller.Workspace(session.Worktree, true)
					return count("Git") == 2 && count("main") == 1 && watched
				}
				return count("Git") == 0 && count("main") == 0
			})
		})
	}
}

func countText(rows []string, value string) int {
	n := 0
	for _, row := range rows {
		if row == value {
			n++
		}
	}
	return n
}
