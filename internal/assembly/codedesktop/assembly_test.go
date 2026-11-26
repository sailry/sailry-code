package codedesktop_test

import (
	"slices"
	"testing"

	"github.com/sailry/sailry-code/internal/assembly/codedesktop"
	databases "github.com/sailry/sailry-code/modules/code/databases/desktop"
	ssh "github.com/sailry/sailry-code/modules/code/ssh/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/harness"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/desktop/settings"
)

func TestAssemblySelectsCodeAndBasePages(t *testing.T) {
	assembly := codedesktop.Assembly()
	var pages []native.Page
	for _, page := range assembly.WorkspacePages {
		pages = append(pages, page.Page)
	}
	base := harness.Assembly()
	want := []native.Page{databases.PageID, ssh.PageID, native.FilesPage, native.GitPage}
	for _, page := range base.WorkspacePages {
		want = append(want, page.Page)
	}
	if !slices.Equal(pages, want) {
		t.Fatalf("workspace page order = %v, want %v", pages, want)
	}
	if len(assembly.MetricContributions) != len(base.MetricContributions) {
		t.Fatal("Code is missing Harness composer and provider metrics")
	}
	if !assembly.HasPanelResource(native.Terminal) || !slices.ContainsFunc(assembly.SettingsPages, func(page settings.NativePage) bool { return page.Section == settings.Terminal }) {
		t.Fatal("Code terminal actions or settings are absent")
	}
	app := native.NewPreview(assembly)
	app.OpenWorkspaceTerminal("project-shell", "Project shell")
	if app.ActiveTerminal() != "project-shell" {
		t.Fatal("Code did not open the terminal workspace pane")
	}
	if assembly.ProviderUI == nil {
		t.Fatal("base provider contribution is absent")
	}
	if len(assembly.ExecutionSettings) != len(base.ExecutionSettings) {
		t.Fatal("Code added execution settings beyond those supplied by Harness")
	}
	if got := len(assembly.ProjectWorkflows); got != 2 {
		t.Fatalf("project workflows = %d, want Git and Worktrees", got)
	}
	catalog := i18n.New(assembly.Locales...)
	for _, language := range []string{"en", "zh-CN"} {
		for _, key := range []string{"git_page", "ssh", "terminal_environment", "terminal_variable"} {
			if catalog.In(language, key) == key {
				t.Errorf("%s is not localized in %s", key, language)
			}
		}
	}
}

func TestUnassembledDesktopHasNoCodeContributions(t *testing.T) {
	empty := desktop.Assemble()
	if len(empty.WorkspacePages) != 0 || len(empty.SettingsPages) != 0 || len(empty.Locales) != 0 || empty.ProviderUI != nil || len(empty.ExecutionSettings) != 0 || empty.HasPanelResource(native.Terminal) || len(empty.ProjectWorkflows) != 0 {
		t.Fatalf("empty assembly contains implicit product contributions: %+v", empty)
	}
}
