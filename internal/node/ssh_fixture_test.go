package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
	cryptossh "golang.org/x/crypto/ssh"
)

const (
	sshLogName      = "ssh-canary-profile"
	sshLogUser      = "ssh-canary-user"
	sshLogPassword  = "ssh-canary-password"
	sshLogBanner    = "SSH-2.0-ssh-canary-banner"
	sshLogHome      = "/home/ssh-canary-remote"
	sshLogStdout    = "ssh-canary-stdout"
	sshLogStderr    = "ssh-canary-stderr"
	sshLogHandshake = "handshake"
	sshLogPlatform  = "uname -s; uname -m"
	sshLogPaths     = "printf '%s\\n' \"$HOME\"; uname -s; id -u"
	sshLogInstall   = "sh -s"
)

type sshLogPeer struct {
	address       *net.TCPAddr
	key           protocol.SshHostKey
	allowPassword atomic.Bool
	passwordSeen  atomic.Bool
	installerRuns atomic.Int32
	pause         atomic.Pointer[sshLogPause]
}

type sshLogPause struct {
	stage   string
	reached chan struct{}
	resume  chan struct{}
}

func newSSHLogPeer(t *testing.T) *sshLogPeer {
	t.Helper()
	signer, err := cryptossh.NewSignerFromKey(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	peer := &sshLogPeer{
		address: listener.Addr().(*net.TCPAddr),
		key:     protocol.SshHostKey{Algorithm: signer.PublicKey().Type(), Fingerprint: cryptossh.FingerprintSHA256(signer.PublicKey())},
	}
	config := &cryptossh.ServerConfig{ServerVersion: sshLogBanner, PasswordCallback: func(conn cryptossh.ConnMetadata, password []byte) (*cryptossh.Permissions, error) {
		if conn.User() == sshLogUser && string(password) == sshLogPassword {
			peer.passwordSeen.Store(true)
			if peer.allowPassword.Load() {
				return nil, nil
			}
		}
		return nil, errors.New("fixture rejected password")
	}}
	config.AddHostKey(signer)
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("SSH log fixture did not stop")
		}
	})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			peer.serve(conn, config)
		}
	}()
	return peer
}

func (peer *sshLogPeer) pauseAt(stage string) *sshLogPause {
	pause := &sshLogPause{stage: stage, reached: make(chan struct{}), resume: make(chan struct{})}
	peer.pause.Store(pause)
	return pause
}

func (peer *sshLogPeer) arrive(stage string) *sshLogPause {
	pause := peer.pause.Load()
	if pause == nil || pause.stage != stage {
		return nil
	}
	peer.pause.Store(nil)
	close(pause.reached)
	return pause
}

func (pause *sshLogPause) wait(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-pause.reached:
	case <-ctx.Done():
		t.Fatalf("SSH did not reach %q: %v", pause.stage, ctx.Err())
	}
}

func (peer *sshLogPeer) serve(conn net.Conn, config *cryptossh.ServerConfig) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if pause := peer.pause.Load(); pause != nil && pause.stage == sshLogHandshake {
		var versionPrefix [4]byte
		if _, err := io.ReadFull(conn, versionPrefix[:]); err != nil || string(versionPrefix[:]) != "SSH-" {
			return
		}
		peer.arrive(sshLogHandshake)
		_, _ = io.Copy(io.Discard, conn)
		return
	}
	server, channels, global, err := cryptossh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer server.Close()
	go cryptossh.DiscardRequests(global)
	for offered := range channels {
		if offered.ChannelType() != "session" {
			_ = offered.Reject(cryptossh.UnknownChannelType, "session expected")
			continue
		}
		channel, requests, err := offered.Accept()
		if err != nil {
			return
		}
		peer.exec(channel, requests)
	}
}

func (peer *sshLogPeer) exec(channel cryptossh.Channel, requests <-chan *cryptossh.Request) {
	defer channel.Close()
	request, ok := <-requests
	if !ok {
		return
	}
	var payload struct{ Command string }
	if request.Type != "exec" || cryptossh.Unmarshal(request.Payload, &payload) != nil {
		_ = request.Reply(false, nil)
		return
	}
	var stdout, stderr string
	var status uint32
	switch payload.Command {
	case sshLogPlatform:
		stdout = "Linux\nx86_64\n"
	case sshLogPaths:
		stdout = sshLogHome + "\nLinux\n1000\n"
	case sshLogInstall:
		stdout, stderr, status = sshLogStdout, sshLogStderr+" "+sshLogHome+"\n", 1
	default:
		_ = request.Reply(false, nil)
		return
	}
	if request.Reply(true, nil) != nil {
		return
	}
	if payload.Command == sshLogInstall {
		if size, err := io.Copy(io.Discard, channel); err != nil || size == 0 {
			return
		}
		peer.installerRuns.Add(1)
	}
	if pause := peer.arrive(payload.Command); pause != nil {
		select {
		case <-pause.resume:
		case <-requests:
			return
		}
	}
	if _, err := io.WriteString(channel, stdout); err != nil {
		return
	}
	if _, err := io.WriteString(channel.Stderr(), stderr); err != nil {
		return
	}
	_, _ = channel.SendRequest("exit-status", false, cryptossh.Marshal(struct{ Status uint32 }{status}))
}
