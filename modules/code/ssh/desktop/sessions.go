package desktop

import (
	"encoding/json"

	"github.com/sailry/sailry-harness/sdk/desktop/view"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

// NewSessionConfig seeds a new SSH resource session. It never inherits a
// saved session's assistant binding or permission.
func NewSessionConfig(profile protocol.SshID, base view.SessionConfig) view.SessionConfig {
	base.Resource = &protocol.ConnectionResource{Kind: "ssh", Data: profile}
	base.Assistant = nil
	base.Permission = "ask"
	return base
}

// SessionMatches reports whether a session belongs to a saved connection's
// history: a live, non-delegated session on that SSH resource. A matching
// session is shown as saved, including a legacy assistant binding.
func SessionMatches(session view.Session, profile protocol.SshID) bool {
	resource := session.Config.Resource
	if session.Archived || session.Delegation != nil || resource == nil || resource.Kind != "ssh" {
		return false
	}
	body, err := json.Marshal(resource.Data)
	var id protocol.SshID
	return err == nil && json.Unmarshal(body, &id) == nil && id == profile
}
