package codedesktop_test

import (
	"slices"
	"testing"

	"github.com/sailry/sailry-code/internal/assembly/codedesktop"
	terminal "github.com/sailry/sailry-code/modules/code/terminal/desktop"
	worktrees "github.com/sailry/sailry-code/modules/code/worktrees/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func TestCodeLegacyPluginControlRegistrations(t *testing.T) {
	terminalControls := []native.PluginControlReplacement{
		{Package: "commands", Slot: "project", ID: "terminal-cli"},
		{Package: "commands", Slot: "project_menu", ID: "terminal-menu-new"},
		{Package: "commands", Slot: "project_menu", ID: "terminal-menu-cli"},
	}
	worktreeControls := []native.PluginControlReplacement{
		{Package: "worktrees", Slot: "project_menu", ID: "manage"},
		{Package: "worktrees", Slot: "composer", ID: "create"},
		{Package: "worktrees", Slot: "composer", ID: "fork"},
	}
	for _, row := range []struct {
		name     string
		assembly native.Assembly
		want     []native.PluginControlReplacement
	}{
		{"empty", native.Assemble(), nil},
		{"terminal", native.Assemble(terminal.Register), terminalControls},
		{"worktrees", native.Assemble(worktrees.Register), worktreeControls},
		{"combined", native.Assemble(terminal.Register, worktrees.Register), slices.Concat(terminalControls, worktreeControls)},
		{"product", codedesktop.Assembly(), slices.Concat(worktreeControls, terminalControls)},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := row.assembly.PluginControlReplacements; !slices.Equal(got, row.want) {
				t.Fatalf("legacy plugin control replacements = %+v, want %+v", got, row.want)
			}
		})
	}
}
