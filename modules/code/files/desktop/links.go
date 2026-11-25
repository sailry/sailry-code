package desktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func registerLinks(assembly *native.Assembly) {
	assembly.RegisterLinkHandler(native.LinkRoute{Kind: native.LinkFile}, func(_ *ui.Context, target native.LinkTarget, actions native.LinkActions) error {
		destination := native.LinkFileDestination{Page: native.FilesPage}
		if target.ConversationWorkspace {
			destination = native.LinkFileDestination{Panel: native.Files}
		}
		return actions.OpenWorkspaceFile(destination)
	})
	assembly.RegisterLinkHandler(native.LinkRoute{Kind: native.LinkDirectory}, func(_ *ui.Context, _ native.LinkTarget, actions native.LinkActions) error {
		return actions.ShowWorkspaceDirectory(native.FilesPage, func(project *native.Project, directory string) {
			project.RevealFileDirectory(directory)
		})
	})
}
