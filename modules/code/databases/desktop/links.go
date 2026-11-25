package desktop

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func registerLinks(assembly *native.Assembly) {
	assembly.RegisterLinkHandler(native.LinkRoute{Kind: native.LinkReference, Name: "database"}, openDatabaseReference)
}

func openDatabaseReference(c *ui.Context, target native.LinkTarget, actions native.LinkActions) error {
	payload, ok := protocol.DataAs[protocol.ConversationReferenceTargetDatabasePayload](target.Reference.Target.Data)
	if ok && actions.DatabaseReference != nil && actions.ShowReference != nil {
		if name, found := actions.DatabaseReference(payload.Connection); found {
			parts := []string{name}
			if payload.Database != nil {
				parts = append(parts, *payload.Database)
			}
			if payload.Table != nil {
				parts = append(parts, payload.Table.Schema+"."+payload.Table.Name)
			}
			actions.ShowReference(target.Reference.Label, i18n.T(c, "reference_databases"), strings.Join(parts, " / "))
			return nil
		}
	}
	return &protocol.Fault{Code: protocol.ErrorCodeUnavailable, Message: "database reference is unavailable"}
}
