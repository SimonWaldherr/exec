package exec_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/stretchr/testify/assert"
	"simonwaldherr.de/go/exec"
)

func TestRunAndGetOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output, err := exec.RunAndGetOutput(ctx, "echo", "hello world")
	assert.NoError(t, err)
	assert.Contains(t, output, "hello world")
}

func TestRunWithChannel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	outputChan, err := exec.RunWithChannel(ctx, "echo", "hello world")
	assert.NoError(t, err)

	var output string
	for line := range outputChan {
		output += line
	}
	assert.Contains(t, output, "hello world")
}

// HoneyPot encapsulates the initialized struct
type HoneyPot struct {
	server *ssh.Server
}

// NewHoneyPot takes in IP address to be used for honeypot
func NewHoneyPot(addr string) *HoneyPot {
	return &HoneyPot{
		server: &ssh.Server{
			Addr: addr,
			Handler: func(s ssh.Session) {
				// if command is "echo 'remote test'"
				// then return "remote test"
				if fmt.Sprint(s.Command()) == "echo 'remote test'" {
					io.WriteString(s, "remote test")
					return
				}
				//io.WriteString(s, "Honey pot")
			},
			PasswordHandler: func(ctx ssh.Context, password string) bool {
				return true
			},
		},
	}
}

// ListenAndServe listens on the TCP network address srv.Addr
// and then calls Serve to handle incoming connections
func (h *HoneyPot) ListenAndServe() error {
	return h.server.ListenAndServe()
}

// Close returns any error returned from closing
// the Server's underlying Listener(s).
func (h *HoneyPot) Close() error {
	return h.server.Close()
}

// SetReturnString takes in a string and set it as
// the response from the server
func (h *HoneyPot) SetReturnString(str string) {
	h.server.Handler = func(s ssh.Session) {
		io.WriteString(s, str)
	}
}

func TestRunRemoteCommand(t *testing.T) {
	const (
		addr = "localhost:2222"

		returnMsg = "remote test"
	)

	hp := NewHoneyPot(addr)

	go func() {
		hp.ListenAndServe()
	}()
	t.Cleanup(func() {
		hp.Close()
	})

	hp.SetReturnString(returnMsg)

	cfg := exec.SSHConfig{
		User:     "username",
		Host:     "localhost",
		Port:     2222,
		Password: "password",
	}

	conn, err := exec.NewRemoteConnection(cfg)
	assert.NoError(t, err)
	defer conn.Close()

	output, err := conn.Run("echo 'remote test'")
	assert.NoError(t, err)
	assert.Contains(t, output, "remote test")
}
