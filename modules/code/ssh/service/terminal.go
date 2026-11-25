package service

import (
	"context"
	"errors"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/sailry/sailry-code/modules/code/ssh/service/transport"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

// sshTerminalTimeout bounds connecting and requesting the remote shell.
const sshTerminalTimeout = 30 * time.Second

// OpenTerminal connects to a saved profile and starts its shell as a
// terminal. The terminal owns the connection from then on; cancel_ssh from
// the same caller stops the opening.
func (node *Service) OpenTerminal(ctx context.Context, caller protocol.NodeID, request protocol.Request, input protocol.CommandOpenSshTerminalPayload) (protocol.SshOutcome, error) {
	if node.options.Terminals == nil {
		return protocol.SshOutcome{}, &protocol.Fault{Code: protocol.ErrorCodeNotConfigured, Message: "terminal module is unavailable"}
	}
	if err := native.ValidateTerminalViewport(input.Launch.Viewport); err != nil {
		return protocol.SshOutcome{}, err
	}
	profile, credential, err := node.readSSH(ctx, input.Profile, input.ExpectedRevision)
	if err != nil {
		return protocol.SshOutcome{}, err
	}
	scoped, finish, err := node.options.Installations.Begin(ctx, caller, request)
	if err != nil {
		return protocol.SshOutcome{}, err
	}
	defer finish()
	timed, cancel := context.WithTimeout(scoped, sshTerminalTimeout)
	defer cancel()
	interrupted := func(failure error) error {
		return sshInterrupted(ctx, timed, failure, "SSH terminal opening cancelled; inspect terminals before retrying", "SSH terminal opening interrupted; inspect terminals before retrying", "SSH terminal opening timed out; inspect the remote process before retrying")
	}
	connection, err := ssh.ConnectWithRecovery(timed, clockwork.NewRealClock(), profile, credential)
	if err != nil {
		var required *ssh.HostKeyRequired
		if errors.As(err, &required) {
			return required.Outcome(), nil
		}
		return protocol.SshOutcome{}, sshConnectionFailure(timed, err)
	}
	// Session requests have no cancellation; closing the connection releases them.
	stop := context.AfterFunc(timed, func() { _ = connection.Close() })
	shell, err := native.OpenSSHTerminal(connection, input.Launch.Viewport)
	if err != nil {
		_ = connection.Close()
		return protocol.SshOutcome{}, interrupted(err)
	}
	if !stop() {
		_ = shell.Close()
		return protocol.SshOutcome{}, interrupted(context.Cause(timed))
	}
	info, err := node.options.Terminals.Start(ctx, native.TerminalLaunch{
		ID: protocol.TerminalID(request.ID), Caller: caller, SSH: &input.Profile, Connection: shell,
		Viewport: input.Launch.Viewport, Appearance: input.Launch.Appearance,
	})
	if err != nil {
		_ = shell.Close()
		return protocol.SshOutcome{}, &protocol.Fault{Code: protocol.ErrorCodeOutcomeUnknown, Message: "SSH shell opened but terminal registration failed: " + err.Error()}
	}
	return protocol.SshOutcome{Kind: "terminal", Data: info}, nil
}
