package codehost

import (
	codeindex "github.com/sailry/sailry-code/modules/code/code-index/node"
	databasesnode "github.com/sailry/sailry-code/modules/code/databases/node"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	sshnode "github.com/sailry/sailry-code/modules/code/ssh/node"
	terminalnode "github.com/sailry/sailry-code/modules/code/terminal/node"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	"github.com/sailry/sailry-harness/sdk/native/node"
)

func Assembly() node.Assembly {
	assembly := node.HarnessAssembly()
	assembly.BundledDefaults = defaultPlugins()
	assembly.Modules = append(assembly.Modules,
		gitnode.Module(),
		worktreesnode.Module(),
		codeindex.Module(),
		databasesnode.Module(),
		sshnode.Module(),
		terminalnode.Module(),
	)
	return assembly
}
