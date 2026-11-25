//go:build visualparity

package visualparity

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/pkg/sftp"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
	cryptossh "golang.org/x/crypto/ssh"
)

const (
	peerUser     = "parity"
	peerPassword = "parity-fixture-password"
	peerHome     = "/srv/parity"
	peerGreeting = "Parity fixture shell ready"
)

// peer is a loopback SSH and SFTP server with a fixed host key, port and
// tree, so a host-key prompt, a terminal greeting and a remote file listing
// look the same in every run without leaving the machine.
type peer struct {
	port uint16
	key  protocol.SshHostKey
}

func startPeer(t *testing.T, port uint16) *peer {
	t.Helper()
	signer, err := cryptossh.NewSignerFromKey(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	files := sftp.InMemHandler()
	for _, dir := range []string{"/srv", peerHome, peerHome + "/deploy", peerHome + "/logs"} {
		if err := files.FileCmd.Filecmd(sftp.NewRequest("Mkdir", dir)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{peerHome + "/README.md", peerHome + "/deploy/run.sh"} {
		request := sftp.NewRequest("Put", name)
		request.Flags = 0x02 | 0x08 | 0x10 // SSH_FXF_WRITE | SSH_FXF_CREAT | SSH_FXF_TRUNC
		writer, err := files.FilePut.Filewrite(request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.WriteAt([]byte("parity\n"), 0); err != nil {
			t.Fatal(err)
		}
		if closer, ok := writer.(io.Closer); ok {
			_ = closer.Close()
		}
	}
	config := &cryptossh.ServerConfig{PasswordCallback: func(conn cryptossh.ConnMetadata, password []byte) (*cryptossh.Permissions, error) {
		if conn.User() != peerUser || string(password) != peerPassword {
			return nil, errors.New("fixture rejected credentials")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("the fixed loopback SSH port %d is unavailable (set SAILRY_PARITY_SSH_PORT): %v", port, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
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
			connections.Go(func() { serve(ctx, conn, config, files) })
		}
	}()
	return &peer{port: port, key: protocol.SshHostKey{Algorithm: signer.PublicKey().Type(), Fingerprint: cryptossh.FingerprintSHA256(signer.PublicKey())}}
}

func serve(ctx context.Context, conn net.Conn, config *cryptossh.ServerConfig, files sftp.Handlers) {
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
		sessions.Go(func() { session(channel, requests, files) })
	}
}

func session(channel cryptossh.Channel, requests <-chan *cryptossh.Request, files sftp.Handlers) {
	defer channel.Close()
	for request := range requests {
		switch request.Type {
		case "pty-req", "window-change", "env":
			_ = request.Reply(true, nil)
		case "shell":
			if request.Reply(true, nil) == nil {
				_, _ = io.WriteString(channel, peerGreeting+"\r\nparity@fixture:~$ ")
			}
		case "subsystem":
			var subsystem struct{ Name string }
			if cryptossh.Unmarshal(request.Payload, &subsystem) == nil && subsystem.Name == "sftp" && request.Reply(true, nil) == nil {
				server := sftp.NewRequestServer(channel, files, sftp.WithStartDirectory(peerHome))
				defer server.Close()
				_ = server.Serve()
				return
			}
			_ = request.Reply(false, nil)
		default:
			_ = request.Reply(false, nil)
		}
	}
}
