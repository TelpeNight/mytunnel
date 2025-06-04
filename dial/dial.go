package dial

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	kh "golang.org/x/crypto/ssh/knownhosts"
	"resenje.org/singleflight"
)

func DialContext(ctx context.Context, addr string) (net.Conn, error) {
	config, err := ParseAddr(addr)
	if err != nil {
		return nil, err
	}
	if err := config.canDial(); err != nil {
		return nil, wrapErr(err)
	}

	sshTun, err := getSshTunnel(ctx, config)
	if err != nil {
		return nil, wrapErr(err)
	}

	dbConn, err := sshTun.sshClient.DialContext(ctx, config.Net, config.Addr)
	if err != nil {
		_ = sshTun.Close()
		return nil, wrapErr(err)
	}

	return &tunnelCon{Conn: dbConn, tunnel: sshTun}, nil
}

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("mytunnel/dial: %w", err)
}

func newSshTunnel(ctx context.Context, config Config) (*sshTunnel, error) {
	if useMockSshClient {
		return &sshTunnel{
			sshClient: newMochSshClient(),
			key:       config.tunnelKey(),
		}, nil
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
	sshCon, err := sshDialCtx(ctx, config.sshAddr(), sshConfig)
	if err != nil {
		return nil, err
	}

	return &sshTunnel{
		sshClient: sshCon,
		key:       config.tunnelKey(),
	}, nil
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

type tunnelCon struct {
	net.Conn
	tunnel    io.Closer
	closeOnce sync.Once
	closeErr  error
}

func (t *tunnelCon) Close() error {
	err1 := t.Conn.Close()
	t.closeOnce.Do(func() {
		t.closeErr = t.tunnel.Close()
	})
	return errors.Join(err1, t.closeErr)
}

type (
	tunnelKey struct {
		Username string
		Host     string
		Port     int
	}
	sshTunnel struct {
		sshClient sshClient
		key       tunnelKey
		users     atomic.Int64 // quick check that tunnel is in use. glocal mutex is locked only when this becomes 0
		closed    bool         // guarded by global mutex
	}
	sshClient interface {
		DialContext(ctx context.Context, net string, addr string) (net.Conn, error)
		Close() error
	}
)

// Close is called once per tunnelCon and guarded by tunnelCon.closeOnce
func (t *sshTunnel) Close() error {
	users := t.users.Add(-1)
	if users > 0 {
		return nil
	}
	return tryCloseSshTunnel(t)
}

func (c Config) tunnelKey() tunnelKey {
	port := c.Port
	if port == 0 {
		port = DefaultPort
	}
	return tunnelKey{
		Username: c.Username,
		Host:     c.Host,
		Port:     port,
	}
}

var tunnelCache = struct {
	mu    sync.Mutex
	cache map[tunnelKey]*sshTunnel
	group singleflight.Group[tunnelKey, *sshTunnel]
}{
	cache: make(map[tunnelKey]*sshTunnel),
}

func getSshTunnel(ctx context.Context, conf Config) (*sshTunnel, error) {
	tunKey := conf.tunnelKey()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if tun, ok := cachedSshTunnel(ctx, conf, true); ok {
			return tun, nil
		}

		tun, _, err := tunnelCache.group.Do(ctx, tunKey, func(ctx context.Context) (*sshTunnel, error) {
			// this goroutine is the only one, who can be in this critical section
			// but another caller could already create the tunnel, so doublecheck just like in a classical singleton pattern
			if tun, ok := cachedSshTunnel(ctx, conf, false); ok {
				return tun, nil
			}

			// so no tunnel in the cache and nobody could add it, except us
			// feel free to create a new one
			tun, err := newSshTunnel(ctx, conf)
			if err != nil {
				return nil, err
			}

			// and store
			tunnelCache.mu.Lock()
			critNoDuplicateKey(ctx, conf, tun)
			tunnelCache.cache[tunKey] = tun
			tunnelCache.mu.Unlock()

			return tun, nil
		})
		if err != nil {
			return nil, err
		}

		// with singleflight, it is possible that the result is shared between multiple goroutine
		// if it is, other goroutine could already close the tunnel, so check it
		// if it is closed, start over
		tunnelCache.mu.Lock()
		if tun.closed {
			tunnelCache.mu.Unlock()
			continue
		}

		// if it is not closed, inc users count. it should be > 0 here, so a tunnel won't be closed even if other goroutine entered `tryCloseSshTunnel`
		users := tun.users.Add(1)
		if users <= 0 {
			// something went wrong. this tunnel is not safe to use
			// close it explicitly and start over
			critTunnelNegUsers(ctx, conf, tun, users-1)
			tunnelCache.mu.Unlock()
			_ = tun.Close()
			continue
		}
		tunnelCache.mu.Unlock()

		return tun, nil
	}
}

func cachedSshTunnel(ctx context.Context, conf Config, addUsers bool) (*sshTunnel, bool) {
	tunnelCache.mu.Lock()
	defer tunnelCache.mu.Unlock()

	tun, ok := tunnelCache.cache[conf.tunnelKey()]
	if !ok {
		return nil, false
	}

	if tun.closed {
		critClosedTunnelInCache(ctx, conf, tun)
		return nil, false
	}

	if addUsers {
		tun.users.Add(1)
	}
	return tun, true
}

func tryCloseSshTunnel(t *sshTunnel) error {
	tunnelCache.mu.Lock()
	users := t.users.Load()
	if users > 0 {
		tunnelCache.mu.Unlock()
		return nil
	}

	critUsersEqZero(t, users)
	critNotClosed(t)
	critInCache(t)

	t.closed = true
	delete(tunnelCache.cache, t.key)
	tunnelCache.mu.Unlock()

	return t.sshClient.Close()
}

// mock for tests

var (
	useMockSshClient = false
	mockClosedCount  atomic.Uint64
)

func newMochSshClient() sshClient {
	return &mochSshClient{}
}

type (
	mochSshClient struct {
		closed atomic.Bool
	}
	mochNetCon struct {
		parent *mochSshClient
		closed atomic.Bool
	}
)

func (m *mochSshClient) DialContext(ctx context.Context, net string, addr string) (net.Conn, error) {
	return &mochNetCon{parent: m}, nil
}

func (m *mochSshClient) Close() error {
	m.closed.Store(true)
	mockClosedCount.Add(1)
	return nil
}

func (m *mochNetCon) Close() error {
	m.closed.Store(true)
	return nil
}

func (m *mochNetCon) Write(b []byte) (n int, err error) {
	if m.parent.closed.Load() {
		panic("accessing closed mochSshClient")
	}
	if m.closed.Load() {
		panic("accessing closed mochNetCon")
	}
	return len(b), nil
}

func (m *mochNetCon) Read(b []byte) (n int, err error) {
	//TODO implement me
	panic("implement me")
}

func (m *mochNetCon) LocalAddr() net.Addr {
	//TODO implement me
	panic("implement me")
}

func (m *mochNetCon) RemoteAddr() net.Addr {
	//TODO implement me
	panic("implement me")
}

func (m *mochNetCon) SetDeadline(t time.Time) error {
	//TODO implement me
	panic("implement me")
}

func (m *mochNetCon) SetReadDeadline(t time.Time) error {
	//TODO implement me
	panic("implement me")
}

func (m *mochNetCon) SetWriteDeadline(t time.Time) error {
	//TODO implement me
	panic("implement me")
}
