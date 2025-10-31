package dial

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"

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

	const dialAttempts = 2
	var lastErr error
	for range dialAttempts {
		sshTun, err := getSshTunnel(ctx, config)
		if err != nil {
			return nil, wrapErr(err)
		}

		tunConn, err := sshTun.client.DialContext(ctx, config.Net, config.Addr)
		if err != nil {
			if errors.Is(err, ctx.Err()) {
				_ = sshTun.release()
				return nil, err
			}
			// if the tunnel fails to dial, count it as invalid
			// close it and start over
			lastErr = err
			sshTun.forget()
			continue
		}

		sshTun.keepAlive(config.Params)
		return &tunnelConn{Conn: tunConn, tunnel: sshTun}, nil
	}

	return nil, wrapErr(lastErr)
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

var clientPool = &sshClientPool{
	m: make(map[clientKey]*clientPoolEntry),
}

func getSshTunnel(ctx context.Context, config Config) (*sshPooledTunnel, error) {
	key := config.clientKey()
	return clientPool.acquire(ctx, key, func(ctx context.Context) (sshClient, error) {
		return newSshClient(ctx, config)
	})
}

func (c Config) clientKey() clientKey {
	port := c.Port
	if port == 0 {
		port = DefaultPort
	}
	return clientKey{
		Username: c.Username,
		Password: passKey(c.Password),
		Host:     c.Host,
		Port:     port,
	}
}

func passKey(password *string) string {
	if password == nil {
		return "-"
	}
	if *password == "" {
		return "*"
	}
	return "-*" + *password
}

type tunnelConn struct {
	net.Conn
	tunnel    *sshPooledTunnel
	closeOnce sync.Once
	closeErr  error
}

func (t *tunnelConn) Close() error {
	err1 := t.Conn.Close()
	t.closeOnce.Do(func() {
		t.closeErr = t.tunnel.release()
	})
	return errors.Join(err1, t.closeErr)
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
}

func newSshClient(ctx context.Context, config Config) (sshClient, error) {
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
	client, _, err := sshDialCtx(ctx, config.sshAddr(), sshConfig)
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

func sshDialCtx(ctx context.Context, addr string, config *ssh.ClientConfig) (*ssh.Client, *netConn, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	type clientErr struct {
		client *ssh.Client
		conn   *netConn
		err    error
	}
	dialDone := make(chan clientErr)
	go func() {
		client, conn, err := sshDial(ctx, addr, config)
		select {
		case dialDone <- clientErr{client, conn, err}:
		case <-ctx.Done():
			if client != nil {
				_ = client.Close()
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case res := <-dialDone:
		return res.client, res.conn, res.err
	}
}

func sshDial(ctx context.Context, addr string, config *ssh.ClientConfig) (*ssh.Client, *netConn, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	if tcp, is := conn.(*net.TCPConn); is {
		errKa := tcp.SetKeepAlive(true)
		if errKa != nil {
			slog.WarnContext(ctx, "mytunnel/dial: cannot set keep alive", "addr", addr, "err", errKa.Error())
		}
	}
	nConn := &netConn{Conn: conn}
	c, chans, reqs, err := ssh.NewClientConn(nConn, addr, config)
	if err != nil {
		// conn is closed on error
		return nil, nil, err
	}
	return ssh.NewClient(c, chans, reqs), nConn, nil
}
