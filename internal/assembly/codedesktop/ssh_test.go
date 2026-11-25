package codedesktop_test

import (
	"slices"
	"testing"

	"github.com/sailry/sailry-code/internal/assembly/codedesktop"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/node"
)

// The SSH page's behavior is tested in modules/code/ssh/desktop. The product
// assembly only selects it: one SSH page, absent from the shared Desktop and
// from the Harness Node.
func TestCodeSSHNativeAssemblyIsExplicit(t *testing.T) {
	assembly := codedesktop.Assembly()
	catalog := i18n.New(assembly.Locales...)
	var pages []native.Page
	for _, page := range assembly.WorkspacePages {
		if catalog.In("en", page.Feature.Label) == "SSH" {
			pages = append(pages, page.Page)
		}
	}
	if len(pages) != 1 {
		t.Fatalf("Code native SSH pages = %v, want one", pages)
	}
	if app := fixtures.NewLive(&fixtures.Data{Workspace: &fixtures.Workspace{}}, native.Assemble()); app.NativePageEnabled(pages[0]) {
		t.Fatal("the shared Desktop exposed SSH without a product contribution")
	}
	if host := node.HarnessAssembly(); host.SSH != nil || slices.ContainsFunc(host.Modules, func(module node.Module) bool { return module.ID() == "ssh" }) {
		t.Fatal("the Harness Node assembly includes the Code SSH workflow")
	}
}
