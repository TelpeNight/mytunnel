package dial

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"
	kh "golang.org/x/crypto/ssh/knownhosts"
)

var clientPool = newClientPool()

func DialContext(ctx context.Context, addr string) (net.Conn, error) {
	config, err := ParseAddr(addr)
	if err != nil {
		return nil, err
	}
	if err = config.canDial(); err != nil {
		return nil, wrapErr(err)
	}

	kaConfig := makeKeepAliveConfig(config.Params)
	if useConnMux(config.Params) {
		return newMuxConn(ctx, config, kaConfig)
	}
	return newClientConn(ctx, config, kaConfig)
}

func useConnMux(params url.Values) bool {
	vals := params["ConnMux"]
	switch len(vals) {
	case 0:
		return true
	case 1:
	default:
		logger().Warn("mytunne/dial: multiple values for ConnMux, ignore")
		return true
	}
	val, err := strconv.ParseBool(vals[0])
	if err != nil {
		logger().Warn("mytunne/dial: invalid value for ConnMux, ignore", "err", err)
		return true
	}
	return val
}

func newClientConn(ctx context.Context, config Config, kaConfig keepAliveConfig) (net.Conn, error) {
	cli, err := newSshClient(ctx, config, kaConfig.keepAlive())
	if err != nil {
		return nil, wrapErr(err)
	}

	conn, err := cli.DialContext(ctx, config.Net, config.Addr)
	if err != nil {
		_ = cli.Close()
		return nil, wrapErr(err)
	}

	if kaConfig.keepAlive() {
		keepAlive(cli, kaConfig)
	}
	return &clientConn{Conn: conn, cli: cli}, nil
}

func newMuxConn(ctx context.Context, config Config, kaConfig keepAliveConfig) (net.Conn, error) {
	var (
		ka      = kaConfig.keepAlive()
		lastErr error
	)
	for range 2 {
		tunn, err := clientPool.acquire(ctx, config.clientKey(kaConfig),
			func(ctx context.Context) (sshClient, error) {
				return newSshClient(ctx, config, ka)
			},
		)
		if err != nil {
			return nil, wrapErr(err)
		}

		conn, err := tunn.client.DialContext(ctx, config.Net, config.Addr)
		if err != nil {
			// if client can't dial - it is invalid
			// forget it and start over
			// all other connections, multiplexed by this client, will be closed
			tunn.forget()
			lastErr = err
			continue
		}

		if ka {
			tunn.keepAliveOnce.Do(func() {
				keepAlive(tunn.client, kaConfig)
			})
		}

		return &muxClientConn{Conn: conn, tunn: tunn}, nil
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

type clientConn struct {
	net.Conn
	cli io.Closer
}

func (t *clientConn) Close() error {
	return errors.Join(t.Conn.Close(), t.cli.Close())
}

type muxClientConn struct {
	net.Conn
	tunn  *sshPooledTunnel
	close sync.Once
}

func (t *muxClientConn) Close() error {
	var connErr = t.Conn.Close()
	var tunnErr error
	t.close.Do(func() {
		tunnErr = t.tunn.release()
	})
	return errors.Join(connErr, tunnErr)
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

	var (
		sshConfig = &ssh.ClientConfig{
			User:            config.Username,
			HostKeyCallback: hostKeyCallback,
		}
		authDone       func()
		authMethodsErr error
	)
	sshConfig.Auth, authDone, authMethodsErr = makeSshAuth(ctx, home, config)
	if authDone != nil {
		defer authDone()
	}

	// Connect to the SSH Server
	client, err := sshDialCtx(ctx, config.sshAddr(), sshConfig, keepAlive)
	if err != nil {
		if authMethodsErr != nil {
			err = fmt.Errorf("%w; errors in auth process: %s", err, authMethodsErr)
		}
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
