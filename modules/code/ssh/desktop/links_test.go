package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func TestSSHReferenceFollowsModule(t *testing.T) {
	id := protocol.SshID{1}
	target := native.LinkTarget{Route: native.LinkRoute{Kind: native.LinkReference, Name: "ssh"},
		Reference: protocol.ConversationReference{Label: "Build", Target: protocol.ConversationReferenceTarget{Kind: "ssh", Data: &id}}}
	for _, test := range []struct {
		name       string
		assembly   native.Assembly
		registered bool
		allowed    bool
	}{
		{name: "without SSH", allowed: true},
		{name: "referable", assembly: native.Assemble(Register), registered: true, allowed: true},
		{name: "restricted", assembly: native.Assemble(Register), registered: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := fixtures.NewLive(&fixtures.Data{Workspace: &fixtures.Workspace{}}, test.assembly)
			lookedUp, shown := false, false
			actions := native.LinkActions{LinkServices: native.LinkServices{
				SSHReference: func(actual protocol.SshID) (native.SSHReferenceEndpoint, bool) {
					if actual != id {
						t.Fatalf("looked up %v, want %v", actual, id)
					}
					lookedUp = true
					return native.SSHReferenceEndpoint{Username: "ci", Host: "fixture.invalid", Port: 2222}, test.allowed
				},
				ShowReference: func(label, subtitle, text string) {
					shown = true
					if label != "Build" || subtitle != i18n.In("en", "reference_ssh") || text != "ci@fixture.invalid:2222" {
						t.Fatalf("reference details %q / %q / %q", label, subtitle, text)
					}
				},
			}}
			tester := ui.NewTester(func(c *ui.Context) {
				handled, err := app.DispatchLink(c, target, actions)
				if handled != test.registered || (err != nil) != (test.registered && !test.allowed) {
					t.Fatalf("dispatch returned %v, %v", handled, err)
				}
			}, 320, 240)
			tester.SetLocale("en")
			tester.Frame()
			if lookedUp != test.registered || shown != (test.registered && test.allowed) {
				t.Fatalf("profile lookup=%v, details shown=%v", lookedUp, shown)
			}
		})
	}
}
