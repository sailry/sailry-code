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
	browser "github.com/sailry/sailry-harness/modules/browser/desktop"
	computer "github.com/sailry/sailry-harness/modules/computer/desktop"
	connections "github.com/sailry/sailry-harness/modules/connections/desktop"
	connectors "github.com/sailry/sailry-harness/modules/connectors/desktop"
	decision "github.com/sailry/sailry-harness/modules/decision/desktop"
	hooks "github.com/sailry/sailry-harness/modules/hooks/desktop"
	knowledge "github.com/sailry/sailry-harness/modules/knowledge/desktop"
	market "github.com/sailry/sailry-harness/modules/market/desktop"
	media "github.com/sailry/sailry-harness/modules/media/desktop"
	memory "github.com/sailry/sailry-harness/modules/memory/desktop"
	ocr "github.com/sailry/sailry-harness/modules/ocr/desktop"
	providers "github.com/sailry/sailry-harness/modules/providers/desktop"
	runtime "github.com/sailry/sailry-harness/modules/runtime/desktop"
	skills "github.com/sailry/sailry-harness/modules/skills/desktop"
	teams "github.com/sailry/sailry-harness/modules/teams/desktop"
	websearch "github.com/sailry/sailry-harness/modules/web-search/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop"
)

func Assembly() desktop.Assembly {
	return desktop.Assemble(
		// SSH leads the rail after Conversation, where the SSH package's
		// navigation entry was.
		ssh.Register, files.Register, git.Register, databases.Register, market.Register,
		codeindex.Register, worktrees.Register,
		providers.Register, connectors.Register, teams.Register, skills.Register,
		knowledge.Module(), memory.Register, media.Register, decision.Register,
		terminal.Register, websearch.Register, browser.Register, computer.Register,
		connections.Register, hooks.Register, runtime.Register, ocr.Register,
	)
}
