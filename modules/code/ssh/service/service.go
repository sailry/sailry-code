package service

import (
	"context"

	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

type Service struct {
	options   node.SSHServiceOptions
	pool      *sshBrowsePool
	transfers Transfers
}

func New(options node.SSHServiceOptions) node.SSHService {
	return &Service{options: options, pool: newSSHBrowsePool(nil), transfers: Transfers{options.Transfers}}
}

func (service *Service) Execute(ctx context.Context, caller protocol.NodeID, request protocol.Request) (protocol.Output, error) {
	if handled, output, err := service.dispatchSSHResource(ctx, caller, request); handled {
		return output, err
	}
	return service.runHost(ctx, caller, request)
}

func (service *Service) Invalidate(id protocol.SshID) { service.pool.invalidate(id) }

func (service *Service) Close() error { return service.pool.close() }

func (service *Service) readSSH(ctx context.Context, id protocol.SshID, revision uint64) (protocol.SshProfile, protocol.SshCredential, error) {
	return service.options.ReadSSH(ctx, id, revision)
}
