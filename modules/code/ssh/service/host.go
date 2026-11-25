package service

import (
	"context"
	"errors"

	"github.com/sailry/sailry-code/modules/code/ssh/service/transport"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func (node *Service) runHost(ctx context.Context, caller protocol.NodeID, request protocol.Request) (protocol.Output, error) {
	var id protocol.SshID
	var revision uint64
	var expected *protocol.NodeID
	var network *protocol.NetworkSelection
	switch input := request.Command.Data.(type) {
	case *protocol.CommandCheckSshPayload:
		id, revision = input.Profile, input.ExpectedRevision
	case *protocol.CommandInstallHostPayload:
		id, revision, network = input.Profile, input.ExpectedRevision, input.Network
	case *protocol.CommandUpdateSshHostPayload:
		id, revision = input.Profile, input.ExpectedRevision
		expected = &input.Node
	default:
		return protocol.Output{}, &protocol.Fault{Code: protocol.ErrorCodeInvalidRequest, Message: "SSH operation expected"}
	}
	if request.Command.Kind != "check_ssh" && node.options.Link() == nil {
		return protocol.Output{}, &protocol.Fault{Code: protocol.ErrorCodeUnavailable, Message: "Node Link is unavailable"}
	}
	profile, credential, err := node.readSSH(ctx, id, revision)
	if err != nil {
		return protocol.Output{}, err
	}
	scoped, finish, err := node.options.Installations.Begin(ctx, caller, request)
	if err != nil {
		return protocol.Output{}, err
	}
	defer finish()
	connection, err := ssh.Connect(scoped, profile, credential)
	if err != nil {
		var required *ssh.HostKeyRequired
		if errors.As(err, &required) {
			return protocol.Output{Kind: "ssh_outcome", Data: required.Outcome()}, nil
		}
		return protocol.Output{}, sshConnectionFailure(scoped, err)
	}
	defer connection.Close()
	if request.Command.Kind == "check_ssh" {
		return protocol.Output{Kind: "ssh_outcome", Data: protocol.SshOutcome{Kind: "connected"}}, nil
	}
	outcome, err := node.options.Installations.Execute(scoped, connection, node.options.Link(), caller, request.ID, expected, network)
	return protocol.Output{Kind: "ssh_outcome", Data: outcome}, err
}
