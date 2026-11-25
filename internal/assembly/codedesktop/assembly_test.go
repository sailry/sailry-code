package codedesktop_test

import (
	"slices"
	"testing"

	"github.com/sailry/sailry-code/internal/assembly/codedesktop"
	git "github.com/sailry/sailry-code/modules/code/git/desktop"
	terminal "github.com/sailry/sailry-code/modules/code/terminal/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop"
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
	if !slices.Equal(pages, []native.Page{native.FilesPage, native.GitPage, "page:settings_plugins"}) {
		t.Fatalf("workspace page order = %v", pages)
	}
	if !slices.ContainsFunc(assembly.SettingsPages, func(page settings.NativePage) bool { return page.Section == settings.Terminal }) {
		t.Fatal("Code terminal settings are absent")
	}
	if assembly.ProviderUI == nil {
		t.Fatal("base provider contribution is absent")
	}
	if !assembly.CodeIndex {
		t.Fatal("Code index settings are absent")
	}
	if !assembly.Worktrees {
		t.Fatal("Code worktree actions are absent")
	}
	catalog := i18n.New(assembly.Locales...)
	for _, language := range []string{"en", "zh-CN"} {
		for _, key := range []string{"git_page", "terminal_environment", "terminal_variable"} {
			if catalog.In(language, key) == key {
				t.Errorf("%s is not localized in %s", key, language)
			}
		}
	}
}

func TestCodeContributionsRemainOptional(t *testing.T) {
	gitOnly := desktop.Assemble(git.Register)
	if len(gitOnly.WorkspacePages) != 1 || len(gitOnly.SettingsPages) != 0 {
		t.Fatalf("Git-only assembly = %+v", gitOnly)
	}
	terminalOnly := desktop.Assemble(terminal.Register)
	if len(terminalOnly.WorkspacePages) != 0 || len(terminalOnly.SettingsPages) != 1 {
		t.Fatalf("terminal-only assembly = %+v", terminalOnly)
	}
	empty := desktop.Assemble()
	if len(empty.WorkspacePages) != 0 || len(empty.SettingsPages) != 0 || len(empty.Locales) != 0 || empty.ProviderUI != nil || empty.CodeIndex || empty.Worktrees {
		t.Fatalf("empty assembly contains implicit product contributions: %+v", empty)
	}
}
