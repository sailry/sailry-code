package desktop_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pkg/sftp"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
	cryptossh "golang.org/x/crypto/ssh"
)

const (
	codeSSHUser     = "code-ssh-fixture"
	codeSSHPassword = "fixture-ssh-password"
	codeSSHHome     = "/workspace"
	codeSSHGreeting = "Code SSH fixture shell ready"
	codeSSHFile     = "notes.txt"
	codeSSHContents = "remote fixture notes\n"
	codeSSHCommand  = "fixture-ssh-status"
	codeSSHOutput   = "remote fixture command\n"
)

type codeSSHPeer struct {
	port            uint16
	key             protocol.SshHostKey
	files           sftp.Handlers
	authentications atomic.Int32
	shells          atomic.Int32
	commands        atomic.Int32
}

func newCodeSSHPeer(t *testing.T) *codeSSHPeer {
	t.Helper()
	signer, err := cryptossh.NewSignerFromKey(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	peer := &codeSSHPeer{
		key:   protocol.SshHostKey{Algorithm: signer.PublicKey().Type(), Fingerprint: cryptossh.FingerprintSHA256(signer.PublicKey())},
		files: sftp.InMemHandler(),
	}
	for _, dir := range []string{codeSSHHome, codeSSHHome + "/docs"} {
		if err := peer.files.FileCmd.Filecmd(sftp.NewRequest("Mkdir", dir)); err != nil {
			t.Fatal(err)
		}
	}
	const sftpWrite, sftpCreate = 0x02, 0x08
	file := sftp.NewRequest("Put", codeSSHHome+"/docs/"+codeSSHFile)
	file.Flags = sftpWrite | sftpCreate
	writer, err := peer.files.FilePut.Filewrite(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteAt([]byte(codeSSHContents), 0); err != nil {
		t.Fatal(err)
	}
	config := &cryptossh.ServerConfig{PasswordCallback: func(conn cryptossh.ConnMetadata, password []byte) (*cryptossh.Permissions, error) {
		peer.authentications.Add(1)
		if conn.User() != codeSSHUser || string(password) != codeSSHPassword {
			return nil, errors.New("fixture rejected credentials")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	peer.port = uint16(listener.Addr().(*net.TCPAddr).Port)
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = listener.Close()
		cancel()
		<-done
	})
	go func() {
		defer close(done)
		var connections sync.WaitGroup
		defer connections.Wait()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Go(func() { peer.serve(ctx, conn, config) })
		}
	}()
	return peer
}

func (peer *codeSSHPeer) serve(ctx context.Context, conn net.Conn, config *cryptossh.ServerConfig) {
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	server, channels, global, err := cryptossh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	var sessions sync.WaitGroup
	defer func() {
		_ = server.Close()
		sessions.Wait()
	}()
	go cryptossh.DiscardRequests(global)
	for offer := range channels {
		if offer.ChannelType() != "session" {
			_ = offer.Reject(cryptossh.UnknownChannelType, "session only")
			continue
		}
		channel, requests, err := offer.Accept()
		if err != nil {
			return
		}
		sessions.Go(func() { peer.session(channel, requests) })
	}
}

func (peer *codeSSHPeer) session(channel cryptossh.Channel, requests <-chan *cryptossh.Request) {
	defer channel.Close()
	for request := range requests {
		switch request.Type {
		case "pty-req", "window-change":
			_ = request.Reply(true, nil)
		case "shell":
			if request.Reply(true, nil) == nil {
				peer.shells.Add(1)
				_, _ = io.WriteString(channel, codeSSHGreeting+"\r\n")
			}
		case "exec":
			var input struct{ Command string }
			if cryptossh.Unmarshal(request.Payload, &input) != nil || input.Command != codeSSHCommand {
				_ = request.Reply(false, nil)
				return
			}
			if request.Reply(true, nil) == nil {
				peer.commands.Add(1)
				_, _ = io.WriteString(channel, codeSSHOutput)
				_, _ = channel.SendRequest("exit-status", false, cryptossh.Marshal(struct{ Status uint32 }{}))
			}
			return
		case "subsystem":
			var subsystem struct{ Name string }
			if cryptossh.Unmarshal(request.Payload, &subsystem) == nil && subsystem.Name == "sftp" {
				if request.Reply(true, nil) == nil {
					server := sftp.NewRequestServer(channel, peer.files, sftp.WithStartDirectory(codeSSHHome))
					defer server.Close()
					_ = server.Serve()
				}
				return
			}
			_ = request.Reply(false, nil)
		default:
			_ = request.Reply(false, nil)
		}
	}
}
