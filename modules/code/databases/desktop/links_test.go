package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func TestDatabaseReferenceFollowsModule(t *testing.T) {
	id := protocol.DatabaseID{1}
	value := protocol.ConversationReferenceTargetDatabasePayload{Connection: id, Database: new("main"),
		Table: &protocol.DatabaseTable{Schema: "public", Name: "invoices"}}
	target := native.LinkTarget{Route: native.LinkRoute{Kind: native.LinkReference, Name: "database"},
		Reference: protocol.ConversationReference{Label: "Invoices", Target: protocol.ConversationReferenceTarget{Kind: "database", Data: value}}}
	for _, test := range []struct {
		name       string
		assembly   native.Assembly
		registered bool
		allowed    bool
	}{
		{name: "without Databases", allowed: true},
		{name: "referable", assembly: native.Assemble(Register), registered: true, allowed: true},
		{name: "restricted", assembly: native.Assemble(Register), registered: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := fixtures.NewLive(&fixtures.Data{Workspace: &fixtures.Workspace{}}, test.assembly)
			lookedUp, shown := false, false
			actions := native.LinkActions{LinkServices: native.LinkServices{
				DatabaseReference: func(actual protocol.DatabaseID) (string, bool) {
					if actual != id {
						t.Fatalf("looked up %v, want %v", actual, id)
					}
					lookedUp = true
					return "billing", test.allowed
				},
				ShowReference: func(label, subtitle, text string) {
					shown = true
					if label != "Invoices" || subtitle != i18n.In("en", "reference_databases") || text != "billing / main / public.invoices" {
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
