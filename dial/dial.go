package dial

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
	kh "golang.org/x/crypto/ssh/knownhosts"
)

var (
	ErrUserRequired = errors.New("username is required")
	ErrHostRequired = errors.New("host is required")
	ErrPathRequired = errors.New("path is required")
)

func DialContext(ctx context.Context, addr string) (net.Conn, error) {
	addr = strings.ReplaceAll(addr, "(a)", "@")
	userinfo, url, has := strings.Cut(addr, "@")
	if !has || userinfo == "" {
		return nil, wrapErr(ErrUserRequired)
	}
	if url == "" {
		return nil, wrapErr(ErrHostRequired)
	}
	username, password, _ := strings.Cut(userinfo, ":")

	pathIndex := strings.Index(url, "/")
	if pathIndex == -1 {
		return nil, wrapErr(ErrPathRequired)
	}
	host := url[:pathIndex]
	path := url[pathIndex:]
	if host == "" {
		return nil, wrapErr(ErrHostRequired)
	}
	if path == "" {
		return nil, wrapErr(ErrPathRequired)
	}
	portIndex := strings.LastIndex(host, ":")
	if portIndex == -1 {
		host = host + ":22"
	} else {
		port := host[portIndex+1:]
		_, err := strconv.Atoi(port)
		if err != nil {
			return nil, wrapErr(fmt.Errorf("invalid port %q: %w", port, err))
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, wrapErr(fmt.Errorf("cannot determine home directory: %w", err))
	}
	hostKeyCallback, err := kh.New(filepath.Join(home, ".ssh/known_hosts"))
	_ = hostKeyCallback
	if err != nil {
		return nil, wrapErr(err)
	}
	// The client configuration with configuration option to use the ssh-agent
	sshConfig := &ssh.ClientConfig{
		User:            username,
		HostKeyCallback: hostKeyCallback,
	}

	// Try local private key
	if privateKey, err := os.ReadFile(filepath.Join(home, ".ssh/id_rsa")); err == nil {
		signer, err := ssh.ParsePrivateKey(privateKey)
		if err != nil {
			return nil, wrapErr(err)
		}
		sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeys(signer))
	} else if !os.IsNotExist(err) {
		return nil, wrapErr(err)
	} /*else if conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		// When the agentClient connection succeeded, add them as AuthMethod
		// Establish a connection to the local ssh-agent
		defer conn.Close()

		// Create a new instance of the ssh agent
		agentClient := agent.NewClient(conn)
		sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeysCallback(agentClient.Signers))
	}*/

	// When there's a non empty password add the password AuthMethod
	if password != "" {
		sshConfig.Auth = append(sshConfig.Auth, ssh.PasswordCallback(func() (string, error) {
			return password, nil
		}))
	}

	// Connect to the SSH Server
	sshCon, err := sshDialCtx(ctx, host, sshConfig)
	if err != nil {
		return nil, wrapErr(err)
	}

	dbConn, err := sshCon.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, wrapErr(err)
	}

	return &tunnelCon{dbConn, sshCon}, nil
}

func sshDialCtx(ctx context.Context, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	type clientErr struct {
		client *ssh.Client
		err    error
	}
	var dialDone = make(chan clientErr)
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
	tunnel io.Closer
}

func (t *tunnelCon) Close() error {
	err1 := t.Conn.Close()
	err2 := t.tunnel.Close()
	return errors.Join(err1, err2)
}

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("mytunnel.dial: %w", err)
}
