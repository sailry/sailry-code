package desktop

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func registerLinks(assembly *native.Assembly) {
	assembly.RegisterLinkHandler(native.LinkRoute{Kind: native.LinkReference, Name: "ssh"}, openSSHReference)
}

func openSSHReference(c *ui.Context, target native.LinkTarget, actions native.LinkActions) error {
	id, ok := protocol.DataAs[protocol.SshID](target.Reference.Target.Data)
	if ok && actions.SSHReference != nil && actions.ShowReference != nil {
		if endpoint, found := actions.SSHReference(id); found {
			text := endpoint.Username + "@" + endpoint.Host + ":" + strconv.FormatUint(uint64(endpoint.Port), 10)
			actions.ShowReference(target.Reference.Label, i18n.T(c, "reference_ssh"), text)
			return nil
		}
	}
	return &protocol.Fault{Code: protocol.ErrorCodeUnavailable, Message: "SSH reference is unavailable"}
}
