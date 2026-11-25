package codedesktop_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestRailFilesAndGitProjection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	profile := filepath.Join(t.TempDir(), "profile")
	environment := process.NewEnvironment([]string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()})
	owner, err := node.StartWithOptions(ctx, profile, node.Options{Assembly: codehost.Assembly(), Environment: &environment})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Stop(context.Background())
	window, err := fixtures.Connect(ctx, owner, profile, codedesktop.Assembly())
	if err != nil {
		t.Fatal(err)
	}
	defer window.Close()
	controller, app := window.Controller, window.App
	tester := ui.NewTesterWith(app.View, 1280, 820, ui.TesterOptions{Preferences: ui.Preferences{ReduceTransparency: true}})
	tester.SetLocale("en")
	tester.SetScale(0.125)
	text := func(key string) string { return i18n.In("en", key) }
	settle := func() {
		t.Helper()
		for range 6 {
			time.Sleep(time.Millisecond)
			tester.Advance(200 * time.Millisecond)
			tester.Frame()
		}
	}
	wait := func(what string, done func() bool) {
		t.Helper()
		for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			settle()
			if done() {
				return
			}
		}
		t.Fatalf("timed out waiting for %s; visible text: %q", what, tester.Texts())
	}
	rail := func(key string) {
		t.Helper()
		for _, info := range tester.AccessAll() {
			if info.Role == ui.RoleButton && info.Name == text(key) && info.Bounds.X+info.Bounds.W <= native.RailWidth {
				tester.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
				settle()
				return
			}
		}
		t.Fatalf("no %q on the rail: %q", text(key), tester.Texts())
	}
	tree := func(name string) (ui.AccessInfo, bool) { return tester.AccessByRole(ui.RoleTreeItem, name) }
	choose := func(name string) {
		t.Helper()
		row, found := tree(name)
		if !found {
			t.Fatalf("no row %q: %q", name, tester.Texts())
		}
		tester.ClickAt(row.Bounds.X+row.Bounds.W/2, row.Bounds.Y+row.Bounds.H/2)
		settle()
	}
	code := func(want string) bool {
		for _, info := range tester.AccessAll() {
			if strings.Contains(info.Value, want) {
				return true
			}
		}
		return false
	}
	settle()
	if tester.HasText(text("close")) {
		if err := tester.Click(text("close")); err != nil {
			t.Fatal(err)
		}
		settle()
	}

	if !strings.HasPrefix(app.Sidebar.Selected, "draft/") {
		t.Fatalf("a new profile did not open on a draft: %q", app.Sidebar.Selected)
	}
	for _, page := range []struct {
		feature string
		page    native.Page
	}{{"files", native.FilesPage}, {"git", native.GitPage}} {
		rail(page.feature)
		if app.Page != page.page || !tester.HasText(text("resource_project_empty")) || tester.HasText(text("unavailable")) {
			t.Fatalf("the %s page without a project: page %q, %q", page.feature, app.Page, tester.Texts())
		}
	}
	if err := tester.Click(text("project_new")); err != nil {
		t.Fatalf("no New project on the empty page: %v; %q", err, tester.Texts())
	}
	settle()
	if !app.Sidebar.Editing() || !tester.HasText(text("project_name")) {
		t.Fatalf("New project did not open the project editor: %q", tester.Texts())
	}
	tester.Key(0, ui.KeyEscape)
	settle()
	if app.Sidebar.Editing() {
		t.Fatal("Escape left the project editor open")
	}

	folder, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", folder, "-c", "user.name=Rail", "-c", "user.email=rail@example.invalid", "-c", "commit.gpgsign=false"}, arguments...)...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("notes.txt", "first line\n")
	git("init", "-q")
	git("add", "notes.txt")
	git("commit", "-q", "-m", "Add the notes")
	write("notes.txt", "first line\nrail changed line\n")
	write("main.go", "package railpages\n")
	do := func(command protocol.Command) protocol.Output {
		t.Helper()
		output, err := controller.Do(ctx, command)
		if err != nil {
			t.Fatalf("%s: %v", command.Kind, err)
		}
		return output
	}
	project, valid := railDetail[protocol.Project](do(protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
		Name: "Rail workspace", Path: folder, Appearance: protocol.DefaultProjectsAppearance(), Source: protocol.ProjectsSource{Kind: "local"},
	}}).Data)
	if !valid {
		t.Fatal("the registered project was not returned")
	}
	provider := protocol.DefaultConversationProvider()
	provider.ID = protocol.NewProviderID()
	provider.Name, provider.Api = "Rail fixture", protocol.ConversationModelApiChatCompletions
	provider.Authentication, provider.Endpoint = protocol.AuthenticationApiKey, "http://127.0.0.1:9/v1"
	provider.Enabled, provider.DefaultModel = true, "fixture"
	model := protocol.DefaultConversationModel()
	model.ID, model.Context, model.Output = "fixture", 200000, 8192
	model.Reasoning, model.DefaultEffort.Kind = true, "none"
	model.Efforts = append(model.Efforts[:0], model.DefaultEffort)
	provider.Models = []protocol.ConversationModel{model}
	secret := protocol.NewSecret("fixture-key")
	do(protocol.Command{Kind: "save_provider", Data: protocol.CommandSaveProviderPayload{Provider: provider, Secret: &secret}})
	config := protocol.DefaultSessionConfig()
	config.Provider, config.Model, config.Effort = provider.ID, model.ID, model.DefaultEffort
	config.Mode, config.Permission = protocol.WorkModeCode, protocol.PermissionFull
	session, valid := railDetail[protocol.Session](do(protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &project.ID, Config: &config}}).Data)
	if !valid {
		t.Fatal("the created session was not returned")
	}
	wait("the session in the sidebar", func() bool {
		window.OpenSession(session.ID.String())
		workspace := app.Hooks.WorkspaceProject()
		return app.Sidebar.Selected == session.ID.String() && workspace != nil && workspace.ID == session.Worktree.String()
	})
	if app.PanelOpen || len(app.Panel.Tabs) != 0 {
		t.Fatalf("the panel before the pages: open %v, tabs %v", app.PanelOpen, app.Panel.Tabs)
	}

	rail("files")
	wait("the worktree's files on the Files page", func() bool {
		_, listed := tree("main.go")
		return app.Page == native.FilesPage && listed
	})
	if tester.HasText(text("unavailable")) || tester.HasText(text("resource_project_empty")) {
		t.Fatalf("the Files page of a project shows no content: %q", tester.Texts())
	}
	choose("main.go")
	wait("the file's text on the Files page", func() bool { return code("package railpages") })
	pr := app.Hooks.WorkspaceProject()
	if pr == nil || pr.Files.File() != "main.go" {
		t.Fatalf("the Files page did not show the chosen file: %v", pr)
	}
	pr.Actions.OpenFileAt("notes.txt", 2)
	wait("the search result on the Files page", func() bool { return code("rail changed line") })
	if pr.Files.File() != "notes.txt" || app.Page != native.FilesPage || app.PanelOpen {
		t.Fatalf("the search result: file %q, page %q, panel %v", pr.Files.File(), app.Page, app.PanelOpen)
	}

	rail("git")
	wait("the worktree's changes on the Git page", func() bool {
		_, listed := tree("notes.txt")
		return app.Page == native.GitPage && listed
	})
	if tester.HasText(text("unavailable")) || tester.HasText(text("resource_project_empty")) {
		t.Fatalf("the Git page of a project shows no content: %q", tester.Texts())
	}
	choose("notes.txt")
	wait("the diff on the Git page", func() bool { return pr.Git.Change() == "notes.txt" && code("rail changed line") })
	if app.PanelOpen {
		t.Fatal("the pages opened the right panel")
	}

	rail("shell_sessions")
	app.PanelOpen = true
	settle()
	for _, want := range []struct{ kind, path string }{{"files", "notes.txt"}, {"git", "notes.txt"}} {
		found := false
		for _, open := range app.Panel.Tabs {
			found = found || string(open.Kind) == want.kind && open.ID == want.path
		}
		if !found {
			t.Fatalf("the panel does not list the page's %v: %v", want, app.Panel.Tabs)
		}
	}
}

func railDetail[Value any](input any) (Value, bool) {
	switch value := input.(type) {
	case Value:
		return value, true
	case *Value:
		if value != nil {
			return *value, true
		}
	}
	var empty Value
	return empty, false
}
