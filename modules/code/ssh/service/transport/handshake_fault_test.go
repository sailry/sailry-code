package ssh

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func TestConnectReportsHandshakeFailure(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Bool
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	go func() {
		defer close(done)
		if connection, err := listener.Accept(); err == nil {
			accepted.Store(true)
			_ = connection.Close()
		}
	}()
	profile := protocol.SshProfile{
		ID: protocol.NewSshID(), Name: "Fixture", Host: "127.0.0.1", Port: uint16(listener.Addr().(*net.TCPAddr).Port),
		Username: "fixture", Authentication: protocol.SshAuthenticationPassword,
	}
	credential := protocol.SshCredential{Kind: "password", Data: protocol.SshCredentialPasswordPayload{Password: protocol.NewSecret("fixture")}}
	connection, err := Connect(t.Context(), profile, credential)
	if connection != nil {
		_ = connection.Close()
		t.Fatal("incomplete SSH handshake returned a connection")
	}
	var fault *protocol.Fault
	if !errors.As(err, &fault) || fault.Code != protocol.ErrorCodeUnavailable || fault.SSHReason != protocol.SSHHandshakeFailed {
		t.Fatalf("SSH handshake fault = %+v, want %s", err, protocol.SSHHandshakeFailed)
	}
	if !accepted.Load() {
		t.Fatal("SSH check did not connect to the fixture listener")
	}
}
