package dial

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	kh "golang.org/x/crypto/ssh/knownhosts"
)

func DialContext(ctx context.Context, addr string) (net.Conn, error) {
	config, err := ParseAddr(addr)
	if err != nil {
		return nil, err
	}
	if err = config.canDial(); err != nil {
		return nil, wrapErr(err)
	}

	kaConfig := makeKeepAliveConfig(config.Params)
	cli, err := newSshClient(ctx, config, kaConfig.keepAlive())
	if err != nil {
		return nil, wrapErr(err)
	}

	conn, err := cli.DialContext(ctx, config.Net, config.Addr)
	if err != nil {
		_ = cli.Close()
		return nil, wrapErr(err)
	}

	keepAlive(cli, kaConfig)
	return &clientConn{Conn: conn, cli: cli}, nil
}

func (c Config) canDial() error {
	var errs []error
	if c.Username == "" {
		errs = append(errs, ErrUserRequired)
	}
	if c.Host == "" {
		errs = append(errs, ErrHostRequired)
	}
	if c.Net == "" || c.Addr == "" {
		errs = append(errs, ErrAddrRequired)
	}
	return errors.Join(errs...)
}

type clientConn struct {
	net.Conn
	cli io.Closer
}

func (t *clientConn) Close() error {
	return errors.Join(t.Conn.Close(), t.cli.Close())
}

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("mytunnel/dial: %w", err)
}

type sshClient interface {
	DialContext(ctx context.Context, net string, addr string) (net.Conn, error)
	SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error)
	Close() error
	Wait() error
	successfulRead() <-chan struct{}
}

type sshClientConn struct {
	*ssh.Client
	conn *netConn
}

func (c *sshClientConn) successfulRead() <-chan struct{} {
	return c.conn.readCh
}

func newSshClient(ctx context.Context, config Config, keepAlive bool) (sshClient, error) {
	if useMockSshClient {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return newMockSshClient(), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	hostKeyCallback, err := kh.New(filepath.Join(home, ".ssh/known_hosts"))
	if err != nil {
		return nil, err
	}
	// The client configuration with configuration option to use the ssh-agent
	sshConfig := &ssh.ClientConfig{
		User:            config.Username,
		HostKeyCallback: hostKeyCallback,
	}

	// Try local private key
	if privateKey, err := os.ReadFile(filepath.Join(home, ".ssh/id_rsa")); err == nil {
		signer, err := ssh.ParsePrivateKey(privateKey)
		if err != nil {
			return nil, err
		}
		sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeys(signer))
	} else if !os.IsNotExist(err) {
		return nil, err
	} /*else if conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		// When the agentClient connection succeeded, add them as AuthMethod
		// Establish a connection to the local ssh-agent
		defer conn.Close()

		// Create a new instance of the ssh agent
		agentClient := agent.NewClient(conn)
		sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeysCallback(agentClient.Signers))
	}*/

	// When there's a non empty password add the password AuthMethod
	if config.Password != nil {
		sshConfig.Auth = append(sshConfig.Auth, ssh.PasswordCallback(func() (string, error) {
			return *config.Password, nil
		}))
	}

	// Connect to the SSH Server
	client, err := sshDialCtx(ctx, config.sshAddr(), sshConfig, keepAlive)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (c Config) sshAddr() string {
	port := c.Port
	if port == 0 {
		port = DefaultPort
	}
	return fmt.Sprintf("%s:%d", c.Host, port)
}

func sshDialCtx(ctx context.Context, addr string, config *ssh.ClientConfig, keepAlive bool) (*sshClientConn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d := net.Dialer{
		KeepAliveConfig: net.KeepAliveConfig{Enable: true},
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	nConn := &netConn{Conn: conn}
	if keepAlive {
		nConn.readCh = make(chan struct{}, 1)
	}
	nConn.onOpen()

	type clientErr struct {
		client *ssh.Client
		err    error
	}
	clientDone := make(chan clientErr)
	go func() {
		client, err := sshNewClient(nConn, addr, config)
		select {
		case clientDone <- clientErr{client, err}:
		case <-ctx.Done():
			if client != nil {
				_ = client.Close()
			}
		}
	}()

	select {
	case <-ctx.Done():
		_ = nConn.Close()
		return nil, ctx.Err()
	case res := <-clientDone:
		if res.err != nil {
			nConn.onFail(err)
			_ = nConn.Close()
			return nil, res.err
		}
		return &sshClientConn{res.client, nConn}, nil
	}
}

func sshNewClient(conn net.Conn, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	// conn will be closed on <-ctx.Done()
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		// conn is closed on error
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}
