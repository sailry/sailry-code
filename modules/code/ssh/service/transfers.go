package service

import (
	"context"
	"os"

	"github.com/pkg/sftp"
	"github.com/sailry/sailry-code/modules/code/ssh/service/transport"
	"github.com/sailry/sailry-harness/sdk/native/files"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
	cryptossh "golang.org/x/crypto/ssh"
)

type Transfers struct{ *files.Transfers }

func (transfers Transfers) StageSSHUpload(ctx context.Context, caller protocol.NodeID, spec protocol.SshUploadSpec) (protocol.SshUpload, error) {
	return transfers.Transfers.StageSSHUpload(ctx, caller, spec, ssh.TransferLimit, ssh.ValidatePath)
}

func (transfers Transfers) FinishSSHUpload(caller protocol.NodeID, client *sftp.Client, profile protocol.SshProfile, path string, stream protocol.StreamID) (uint64, error) {
	return transfers.Transfers.FinishSSHUpload(caller, profile, path, stream, func(file *os.File, size uint64, overwrite bool) (uint64, error) {
		return ssh.Upload(client, path, file, size, overwrite)
	})
}

func (transfers Transfers) DownloadSSH(ctx context.Context, caller protocol.NodeID, client *sftp.Client, path string) (protocol.SshDownload, error) {
	return transfers.Transfers.DownloadSSH(ctx, caller, path, func(file *os.File) (uint64, string, error) {
		return ssh.Download(client, path, file)
	})
}

func (transfers Transfers) TransferSSH(ctx context.Context, connection *cryptossh.Client, root string, access files.FileAccess, transfer protocol.SshTransfer) (uint64, error) {
	return transfers.Transfers.TransferSSH(ctx, root, access, transfer, ssh.TransferLimit, func(source, destination *os.File) (uint64, string, error) {
		client, err := ssh.OpenSFTP(connection)
		if err != nil {
			return 0, "", ssh.AtStage(err, "remote subsystem")
		}
		defer client.Close()
		if source != nil {
			size, err := ssh.TransferUpload(client, source, transfer.Path, transfer.RemotePath)
			return size, "", err
		}
		return ssh.TransferDownload(client, transfer.RemotePath, destination)
	}, ssh.AtStage)
}
