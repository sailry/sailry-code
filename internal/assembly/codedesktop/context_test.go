package codedesktop_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-code/internal/assembly/codedesktop"
	"github.com/sailry/sailry-code/internal/assembly/codehost"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func TestProjectContextWithCodeHost(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile")
	environment := process.NewEnvironment([]string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()})
	owner, err := node.StartWithOptions(t.Context(), profile, node.Options{Assembly: codehost.Assembly(), Environment: &environment})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Stop(context.Background()) })
	window, err := fixtures.Connect(t.Context(), owner, profile, codedesktop.Assembly())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = window.Close() })
	plain, repository := t.TempDir(), t.TempDir()
	if output, err := exec.Command("git", "-C", repository, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	for name, path := range map[string]string{"Plain folder": plain, "Git folder": repository} {
		if _, err := window.Controller.Do(t.Context(), protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
			Name: name, Path: path, Appearance: protocol.DefaultProjectsAppearance(), Source: protocol.ProjectsSource{Kind: "local"},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	window.App.Navigate(native.ConversationPage)
	window.App.Sidebar.NewChat()
	tester := ui.NewTesterWith(window.App.View, 1280, 820, ui.TesterOptions{Preferences: ui.Preferences{ReduceTransparency: true}})
	tester.SetLocale("en")
	tester.SetScale(0.125)
	controls := func() (bar []ui.AccessInfo) {
		for _, info := range tester.AccessAll() {
			if bounds := info.Bounds; info.Role == ui.RoleMenuButton && bounds.H == 24 && bounds.W > 0 && bounds.Y > native.HeaderHeight {
				bar = append(bar, info)
			}
		}
		return
	}
	names := func() (result []string) {
		for _, info := range controls() {
			result = append(result, info.Name)
		}
		return
	}
	await := func(message string, ready func() bool) {
		t.Helper()
		for end := time.Now().Add(10 * time.Second); ; time.Sleep(time.Millisecond) {
			tester.Advance(17 * time.Millisecond)
			tester.Frame()
			if ready() {
				return
			}
			if time.Now().After(end) {
				t.Fatalf("%s: controls %q, texts %q", message, names(), tester.Texts())
			}
		}
	}
	await("the initial chat has more than its project selector", func() bool {
		return slices.Equal(names(), []string{i18n.In("en", "composer_no_project")}) && len(window.App.Data.Projects) == 2
	})
	choose := func(project string) {
		t.Helper()
		bar := controls()
		tester.ClickAt(bar[0].Bounds.X+bar[0].Bounds.W/2, bar[0].Bounds.Y+bar[0].Bounds.H/2)
		var item ui.AccessInfo
		await("the location menu does not offer "+project, func() bool {
			for _, role := range []ui.Role{ui.RoleMenuItem, ui.RoleMenuItemCheckBox, ui.RoleMenuItemRadio} {
				if found, ok := tester.AccessByRole(role, project); ok {
					item = found
					return true
				}
			}
			return false
		})
		tester.ClickAt(item.Bounds.X+item.Bounds.W/2, item.Bounds.Y+item.Bounds.H/2)
	}
	choose("Plain folder")
	await("a plain directory offers a branch", func() bool { return slices.Equal(names(), []string{"Plain folder"}) })
	choose("Git folder")
	await("the Code Git context lacks its project or main branch", func() bool {
		return slices.Equal(names(), []string{"Git folder", "main"})
	})
	bar := controls()
	if bar[0].Bounds.Y != bar[1].Bounds.Y || bar[1].Bounds.X < bar[0].Bounds.X+bar[0].Bounds.W {
		t.Errorf("the project %v and the branch %v are not side by side on one line", bar[0].Bounds, bar[1].Bounds)
	}
	label := func(key string) string { return i18n.In("en", key) }
	menu := func() {
		t.Helper()
		tester.Frame()
		for _, info := range tester.AccessAll() {
			if bounds := info.Bounds; info.Name == "Git folder" && bounds.X >= native.RailWidth && bounds.X+bounds.W <= native.RailWidth+native.NavWidth {
				tester.RightClickAt(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
				tester.Frame()
				return
			}
		}
		t.Fatal("the sidebar has no Git project row")
	}
	menu()
	want := []string{label("workspace_project"), label("project_edit"), "-", label("workspace_new_session"), label("workspace_new_terminal"),
		label("worktree_manage"), "-", label("workspace_copy_path"), "-", label("project_remove")}
	if items := tester.Menu(); !slices.Equal(items, want) {
		t.Fatalf("the Code project menu is %q, want %q", items, want)
	}
	tester.Key(0, ui.KeyEscape)
	actions := window.App.Sidebar.Actions
	if actions.NewTerminal == nil || actions.Worktrees == nil {
		t.Fatal("Code did not bind the project terminal and worktree adapters")
	}
	terminalAction, worktreeAction := actions.NewTerminal, actions.Worktrees
	t.Cleanup(func() { actions.NewTerminal, actions.Worktrees = terminalAction, worktreeAction })
	var asked []string
	actions.NewTerminal = func(project *fixtures.Project) { asked = append(asked, "terminal "+project.Name) }
	actions.Worktrees = func(project *fixtures.Project, operation, _ string) {
		asked = append(asked, operation+" "+project.Name)
	}
	menu()
	if err := tester.ChooseMenuItem(label("workspace_new_terminal")); err != nil {
		t.Fatal(err)
	}
	menu()
	if err := tester.ChooseMenuItem(label("worktree_manage"), label("worktree_create")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asked, []string{"terminal Git folder", "create Git folder"}) {
		t.Fatalf("Code's project entries asked %q", asked)
	}
	for _, enabled := range []bool{false, true} {
		if err := window.SetPluginEnabled(t.Context(), "commands", enabled); err != nil {
			t.Fatal(err)
		}
		await("the commands package did not reach its selected state", func() bool {
			for _, summary := range window.Controller.Snapshot().Plugins {
				if summary.Name == "commands" {
					return summary.Enabled == enabled
				}
			}
			return false
		})
		menu()
		if items := tester.Menu(); slices.Contains(items, label("workspace_new_terminal")) != enabled || !slices.Contains(items, label("worktree_manage")) {
			t.Fatalf("the Code menu with commands enabled=%v is %q", enabled, items)
		}
		tester.Key(0, ui.KeyEscape)
	}
}
