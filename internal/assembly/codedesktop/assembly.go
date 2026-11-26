// Package codedesktop selects the native pages shipped with Sailry Code.
package codedesktop

import (
	codeindex "github.com/sailry/sailry-code/modules/code/code-index/desktop"
	databases "github.com/sailry/sailry-code/modules/code/databases/desktop"
	files "github.com/sailry/sailry-code/modules/code/files/desktop"
	git "github.com/sailry/sailry-code/modules/code/git/desktop"
	ssh "github.com/sailry/sailry-code/modules/code/ssh/desktop"
	terminal "github.com/sailry/sailry-code/modules/code/terminal/desktop"
	worktrees "github.com/sailry/sailry-code/modules/code/worktrees/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/harness"
)

func Assembly() desktop.Assembly {
	modules := []desktop.Module{
		databases.Register, ssh.Register, files.Register, git.Register,
		codeindex.Register, worktrees.Register, terminal.Register,
	}
	return desktop.Assemble(append(modules, harness.Modules()...)...)
}
