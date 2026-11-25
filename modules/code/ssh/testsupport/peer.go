package sshfixture

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
	cryptossh "golang.org/x/crypto/ssh"
)

const (
	ProfileName     = "ssh-canary-profile"
	User            = "ssh-canary-user"
	Password        = "ssh-canary-password"
	Banner          = "SSH-2.0-ssh-canary-banner"
	Home            = "/home/ssh-canary-remote"
	Stdout          = "ssh-canary-stdout"
	Stderr          = "ssh-canary-stderr"
	Handshake       = "handshake"
	PlatformCommand = "uname -s; uname -m"
	PathsCommand    = "printf '%s\\n' \"$HOME\"; uname -s; id -u"
	InstallCommand  = "sh -s"
)

type Peer struct {
	address       *net.TCPAddr
	key           protocol.SshHostKey
	allowPassword atomic.Bool
	passwordSeen  atomic.Bool
	installerRuns atomic.Int32
	pause         atomic.Pointer[Pause]
}

type Pause struct {
	stage   string
	reached chan struct{}
	resume  chan struct{}
}

func NewPeer(t *testing.T) *Peer {
	t.Helper()
	signer, err := cryptossh.NewSignerFromKey(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	peer := &Peer{
		address: listener.Addr().(*net.TCPAddr),
		key:     protocol.SshHostKey{Algorithm: signer.PublicKey().Type(), Fingerprint: cryptossh.FingerprintSHA256(signer.PublicKey())},
	}
	config := &cryptossh.ServerConfig{ServerVersion: Banner, PasswordCallback: func(conn cryptossh.ConnMetadata, password []byte) (*cryptossh.Permissions, error) {
		if conn.User() == User && string(password) == Password {
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

func (peer *Peer) Address() netip.AddrPort { return peer.address.AddrPort() }

func (peer *Peer) HostKey() protocol.SshHostKey { return peer.key }

func (peer *Peer) AllowPassword() { peer.allowPassword.Store(true) }

func (peer *Peer) PasswordSeen() bool { return peer.passwordSeen.Load() }

func (peer *Peer) InstallerRuns() int32 { return peer.installerRuns.Load() }

func (peer *Peer) PauseAt(stage string) *Pause {
	pause := &Pause{stage: stage, reached: make(chan struct{}), resume: make(chan struct{})}
	peer.pause.Store(pause)
	return pause
}

func (peer *Peer) arrive(stage string) *Pause {
	pause := peer.pause.Load()
	if pause == nil || pause.stage != stage {
		return nil
	}
	peer.pause.Store(nil)
	close(pause.reached)
	return pause
}

func (pause *Pause) Wait(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-pause.reached:
	case <-ctx.Done():
		t.Fatalf("SSH did not reach %q: %v", pause.stage, ctx.Err())
	}
}

func (pause *Pause) Resume() { close(pause.resume) }

func (peer *Peer) serve(conn net.Conn, config *cryptossh.ServerConfig) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if pause := peer.pause.Load(); pause != nil && pause.stage == Handshake {
		var versionPrefix [4]byte
		if _, err := io.ReadFull(conn, versionPrefix[:]); err != nil || string(versionPrefix[:]) != "SSH-" {
			return
		}
		peer.arrive(Handshake)
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

func (peer *Peer) exec(channel cryptossh.Channel, requests <-chan *cryptossh.Request) {
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
	case PlatformCommand:
		stdout = "Linux\nx86_64\n"
	case PathsCommand:
		stdout = Home + "\nLinux\n1000\n"
	case InstallCommand:
		stdout, stderr, status = Stdout, Stderr+" "+Home+"\n", 1
	default:
		_ = request.Reply(false, nil)
		return
	}
	if request.Reply(true, nil) != nil {
		return
	}
	if payload.Command == InstallCommand {
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
