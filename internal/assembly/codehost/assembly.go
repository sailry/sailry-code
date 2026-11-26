package codehost

import (
	_ "embed"

	databasesnode "github.com/sailry/sailry-code/modules/code/databases/node"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	sshnode "github.com/sailry/sailry-code/modules/code/ssh/node"
	terminalnode "github.com/sailry/sailry-code/modules/code/terminal/node"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	"github.com/sailry/sailry-harness/sdk/native/node"
)

//go:embed system-prompt.txt
var systemPrompt string

func Assembly() node.Assembly {
	assembly := node.HarnessAssembly()
	assembly.SystemPrompt = systemPrompt
	assembly.BundledDefaults = defaultPlugins()
	assembly.Modules = append(assembly.Modules,
		gitnode.Module(),
		worktreesnode.Module(),
		databasesnode.Module(),
		sshnode.Module(),
		terminalnode.Module(),
	)
	return assembly
}
