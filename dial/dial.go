package dial

import (
	"context"
	"errors"
	"fmt"
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
	for i := 0; i < dialAttempts; i++ {
		sshTun, err := getSshTunnel(ctx, config)
		if err != nil {
			return nil, wrapErr(err)
		}

		dbConn, err := sshTun.sshClient.DialContext(ctx, config.Net, config.Addr)
		if err != nil {
			if errors.Is(err, ctx.Err()) {
				_ = sshTun.release()
				return nil, wrapErr(err)
			}
			// if the tunnel fails to dial, count it as invalid
			// close it and start over
			lastErr = err
			sshTun.forget()
			continue
		}

		return &tunnelCon{Conn: dbConn, tunnel: sshTun}, nil
	}

	return nil, wrapErr(lastErr)
}

var clientPool = &sshClientPool{
	m: make(map[clientKey]*clientPoolEntry),
}

func getSshTunnel(ctx context.Context, config Config) (*sshPooledTunnel, error) {
	key := config.clientKey()
	client, err := clientPool.acquire(ctx, key, func(ctx context.Context) (sshClient, error) {
		return newSshClient(ctx, config)
	})
	if err != nil {
		return nil, err
	}
	return &sshPooledTunnel{client, key}, nil
}

func (c Config) clientKey() clientKey {
	port := c.Port
	if port == 0 {
		port = DefaultPort
	}
	return clientKey{
		Username: c.Username,
		Host:     c.Host,
		Port:     port,
	}
}

type sshPooledTunnel struct {
	sshClient sshClient
	clientKey clientKey
}

func (t *sshPooledTunnel) release() error {
	return clientPool.release(t.clientKey, t.sshClient)
}

func (t *sshPooledTunnel) forget() {
	clientPool.forget(t.clientKey, t.sshClient)
}

type tunnelCon struct {
	net.Conn
	tunnel    *sshPooledTunnel
	closeOnce sync.Once
	closeErr  error
}

func (t *tunnelCon) Close() error {
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
	Close() error
}

func newSshClient(ctx context.Context, config Config) (sshClient, error) {
	if useMockSshClient {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return newMochSshClient(), nil
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
	client, err := sshDialCtx(ctx, config.sshAddr(), sshConfig)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func sshDialCtx(ctx context.Context, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	type clientErr struct {
		client *ssh.Client
		err    error
	}
	dialDone := make(chan clientErr)
	go func() {
		client, err := sshDial(ctx, addr, config)
		select {
		case dialDone <- clientErr{client, err}:
		case <-ctx.Done():
			if client != nil {
				_ = client.Close()
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-dialDone:
		return result.client, result.err
	}
}

func sshDial(ctx context.Context, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		// conn is closed on error
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}
