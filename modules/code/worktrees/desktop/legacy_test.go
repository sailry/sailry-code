package worktreesdesktop_test

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	git "github.com/sailry/sailry-code/modules/code/git/desktop"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	worktrees "github.com/sailry/sailry-code/modules/code/worktrees/desktop"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

const legacyWorktreeDigest = "ae42d37635d7990e29b1654a311ac5ace70e47b9a7f1226721a63482406fbb9e"

var worktreeControls = []native.PluginControlReplacement{
	{Package: "worktrees", Slot: "context", ID: "location"},
	{Package: "worktrees", Slot: "project_menu", ID: "manage"},
	{Package: "worktrees", Slot: "composer", ID: "create"},
	{Package: "worktrees", Slot: "composer", ID: "fork"},
}

func TestWorktreeControlsCoexistWithPackages(t *testing.T) {
	assembly := native.Assemble(worktrees.Register)
	if !slices.Equal(assembly.PluginControlReplacements, worktreeControls) {
		t.Fatalf("worktree control replacements = %+v, want %+v", assembly.PluginControlReplacements, worktreeControls)
	}
	git.Register(&assembly)
	for _, variant := range []string{"without old package", "official 0.1.22", "same-name third party"} {
		t.Run(variant, func(t *testing.T) {
			var source string
			switch variant {
			case "official 0.1.22":
				source = os.Getenv("SAILRY_TEST_LEGACY_WORKTREES_DIR")
				if source == "" {
					t.Skip("SAILRY_TEST_LEGACY_WORKTREES_DIR does not name the archived official package")
				}
			case "same-name third party":
				source = thirdPartyWorktrees(t)
			}
			host := support.StartNode(t, worktreeHost())
			var installed protocol.PluginInfo
			if source != "" {
				access := sdktest.Inspect(host.Owner)
				var err error
				installed, err = access.InstallPlugin(host.Ctx, source, "", "worktrees")
				if err != nil || len(installed.Issues) != 0 || installed.Extension == nil {
					t.Fatalf("install worktrees package: %v, %+v", err, installed.Issues)
				}
				if variant == "official 0.1.22" && (installed.Summary.Version == nil || *installed.Summary.Version != "0.1.22" || installed.Summary.Digest != legacyWorktreeDigest) {
					t.Fatalf("wrong official release: %+v", installed.Summary)
				}
				var controls []native.PluginControlReplacement
				for _, entry := range installed.Extension.Ui {
					controls = append(controls, native.PluginControlReplacement{Package: installed.Summary.Name, Slot: entry.Slot, ID: entry.ID})
				}
				if !slices.Equal(controls, worktreeControls) {
					t.Fatalf("installed control declarations = %+v", controls)
				}
				if err := access.Store().Node.Write(host.Ctx, func(db *sql.Conn) error {
					_, err := sdktest.InstallPlugin(host.Ctx, db, installed, 0, true)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			w := host.Connect(assembly)
			if source != "" {
				stored := support.Result[protocol.PluginInfo](t, w.Do(protocol.Command{Kind: "read_plugin", Data: protocol.CommandReadPluginPayload{Name: "worktrees"}}))
				if sdktest.PackageReference(stored) != sdktest.PackageReference(installed) || !stored.Summary.Enabled || stored.Summary.Compatibility.Kind != "compatible" {
					t.Fatalf("package not installed and enabled alongside Code: %+v", stored.Summary)
				}
			}
			root := worktreeRepository(t)
			linked := filepath.Join(filepath.Dir(root), "linked")
			runGit(t, root, "worktree", "add", "-b", "linked-fixture", linked)
			project := w.CreateProject("Worktree fixture", root)
			session := w.OpenProjectSession(project.ID)
			thirdParty := variant == "same-name third party"

			w.Await("the session is missing its native location and package controls", func() bool {
				return controlCount(w, ui.RoleMenuButton, project.Name) == 1 && controlCount(w, ui.RoleMenuButton, "main") == 1 &&
					(!thirdParty || controlCount(w, ui.RoleMenuButton, "Third-party location") == 1 &&
						controlCount(w, ui.RoleButton, "Third-party create") == 1 && controlCount(w, ui.RoleButton, "Third-party fork") == 1)
			})
			for _, label := range []string{"Worktree", "Create worktree and continue", "Fork in a separate worktree"} {
				if controlCount(w, ui.RoleButton, label)+controlCount(w, ui.RoleMenuButton, label) != 0 {
					t.Errorf("legacy control %q remained visible", label)
				}
			}
			w.Await("the project menu is missing its native or independent package contribution", func() bool {
				items := w.ProjectMenu(project.Name)
				w.Tester.CloseMenu()
				return slices.Contains(items, support.Label("worktree_manage")) && slices.Contains(items, "Third-party manage") == thirdParty &&
					!slices.Contains(items, "Manage worktrees…")
			})
			if thirdParty {
				w.ProjectMenu(project.Name)
				if err := w.Tester.ChooseMenuItem("Third-party manage"); err != nil {
					t.Fatal(err)
				}
				w.Await("the selected project menu item did not invoke its package", func() bool {
					return w.Tester.HasText("Third-party manager invoked")
				})
			}
			w.Await("the native manager submenu did not reopen", func() bool {
				if slices.Contains(w.ProjectMenu(project.Name), support.Label("worktree_manage")) {
					return true
				}
				w.Tester.CloseMenu()
				return false
			})
			if err := w.Tester.ChooseMenuItem(support.Label("worktree_manage"), support.Label("worktree_remove")); err != nil {
				t.Fatal(err)
			}
			w.Await("the native manager did not list the external worktree", func() bool {
				return w.Tester.HasText("linked-fixture") && w.Tester.HasText(support.Label("workspace_worktrees"))
			})
			w.Tester.Key(0, ui.KeyEscape)
			w.OpenSession(session.ID.String())
			w.Await("the session location was not restored", func() bool { return w.App.Sidebar.Selected == session.ID.String() })
			clickControl(t, w, ui.RoleMenuButton, "main")
			clickMenuChoice(t, w, support.Label("location_create"))
			w.Await("the native create form did not open", func() bool {
				return controlCount(w, ui.RoleTextField, support.Label("git_new_branch")) == 1
			})
			clickControl(t, w, ui.RoleButton, support.Label("settings_cancel"))
			cv := w.App.Data.Find(session.ID.String())
			if cv == nil || w.App.Sidebar.Actions.ForkWorktree == nil {
				t.Fatal("the native session worktree fork action was not bound")
			}
			w.App.Sidebar.Actions.ForkWorktree(cv)
			w.Await("the native fork form did not open", func() bool {
				return w.Tester.HasText(support.Label("location_fork")) && controlCount(w, ui.RoleTextField, support.Label("git_new_branch")) == 1
			})
		})
	}
}

func thirdPartyWorktrees(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	manifest := fmt.Sprintf(`{"$schema":%q,"name":"worktrees","version":"1.0.0","extensions":{"dev.sailry.platform":{
  "scope":"host","api_version":"v1","ui":[
    {"id":"location","slot":"context","kind":"menu","label":{"label":"Third-party location"},"handler":"choose","choices":[{"id":"one","label":{"label":"Third-party location choice"}}]},
    {"id":"manage","slot":"project_menu","kind":"button","label":{"label":"Third-party manage"},"handler":"manage"},
    {"id":"create","slot":"composer","kind":"button","label":{"label":"Third-party create"},"handler":"create"},
    {"id":"fork","slot":"composer","kind":"button","label":{"label":"Third-party fork"},"handler":"fork"}
  ],"desktop":{"entry":"dev.sailry.platform/main.js","ui_entry":"dev.sailry.platform/main.js","resources":["dev.sailry.platform/main.js"]}
}}}`, sdktest.ManifestSchema)
	const script = `import {View,div} from 'gpui-kit';
import {context} from 'sailry';
import {nextContributionEvent,publishContributions} from 'sailry/sdk';
import {toast} from 'sailry/ui';
export default class Fixture extends View {
  init(_props,cx) {
    const project = JSON.parse(context()).surface === 'project';
    const ids = project ? ['manage'] : ['location','create','fork'];
    publishContributions(ids.map(id => ({id,enabled:true})));
    if (project) cx.spawn(async () => {
      const event = await nextContributionEvent();
      if (event.id !== 'manage' || event.kind !== 'invoke') return;
      publishContributions([{id:'manage',enabled:true,reply_to:event.sequence}]);
      toast({id:'third-party-manager',message:'Third-party manager invoked',kind:'info'});
    });
  }
  render() { return div(); }
}`
	if err := os.Mkdir(filepath.Join(root, "dev.sailry.platform"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"plugin.json": manifest, "dev.sailry.platform/main.js": script} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func worktreeHost() node.Assembly {
	return node.Assembly{Modules: []node.Module{gitnode.Module(), worktreesnode.Module()}}
}

func worktreeRepository(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "repository")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("baseline\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "note.txt")
	runGit(t, root, "-c", "user.name=Worktree Fixture", "-c", "user.email=worktrees@example.invalid", "commit", "-m", "Baseline")
	return root
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %q: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func controlCount(w *support.Window, role ui.Role, name string) int {
	count := 0
	for _, control := range w.Tester.AccessAll() {
		if control.Role == role && control.Name == name && !control.Offscreen && control.Bounds.W > 0 && control.Bounds.H > 0 {
			count++
		}
	}
	return count
}

func clickControl(t *testing.T, w *support.Window, role ui.Role, name string) {
	t.Helper()
	var chosen ui.AccessInfo
	w.Await("no enabled control "+name, func() bool {
		entry, ok := w.Tester.AccessByRole(role, name)
		chosen = entry
		return ok && !entry.Disabled && !entry.Offscreen && entry.Bounds.W > 0 && entry.Bounds.H > 0
	})
	w.Click(chosen)
}

func clickMenuChoice(t *testing.T, w *support.Window, name string) {
	t.Helper()
	var chosen ui.AccessInfo
	w.Await("no enabled menu choice "+name, func() bool {
		for _, role := range []ui.Role{ui.RoleMenuItem, ui.RoleMenuItemCheckBox, ui.RoleMenuItemRadio} {
			if entry, ok := w.Tester.AccessByRole(role, name); ok && !entry.Disabled && !entry.Offscreen {
				chosen = entry
				return true
			}
		}
		return false
	})
	w.Click(chosen)
}
