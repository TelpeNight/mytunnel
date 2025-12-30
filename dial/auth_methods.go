package dial

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func makeSshAuth(ctx context.Context, home string, config Config) ([]ssh.AuthMethod, func(), error) {
	auth := appendPasswordAuth(nil, config.Password)
	auth, dones, errs := appendPublicKeysAuth(ctx, auth, nil, nil, home)

	return auth,
		func() {
			for _, d := range dones {
				d()
			}
		},
		errors.Join(errs...)
}

func appendPasswordAuth(auth []ssh.AuthMethod, password *string) []ssh.AuthMethod {
	if password == nil {
		return auth
	}
	return append(auth, ssh.Password(*password))
}

func appendPublicKeysAuth(ctx context.Context, auth []ssh.AuthMethod, done []func(), otherErrs []error, home string) ([]ssh.AuthMethod, []func(), []error) {
	signers, errs := appendPrivateKeySigners(nil, nil, home)
	signers, done, errs = appendAgentSigners(ctx, signers, done, errs)
	if len(signers) > 0 {
		auth = append(auth, ssh.PublicKeys(signers...))
	}
	if len(errs) > 0 {
		otherErrs = append(otherErrs, fmt.Errorf("publickey: %w", errors.Join(errs...)))
	}
	return auth, done, otherErrs
}

func appendPrivateKeySigners(signers []ssh.Signer, errs []error, home string) ([]ssh.Signer, []error) {
	sshDirPath := filepath.Join(home, ".ssh")
	sshDir, err := os.Open(sshDirPath)

	if err != nil {
		errs = append(errs, err)
		return signers, errs
	}

	sshFiles, err := sshDir.Readdir(-1)
	_ = sshDir.Close()

	if err != nil {
		errs = append(errs, err)
		return signers, errs
	}

	for _, file := range sshFiles {
		if file.IsDir() {
			continue
		}
		if !strings.HasPrefix(file.Name(), "id_") {
			continue
		}
		if strings.HasSuffix(file.Name(), ".pub") {
			continue
		}
		filePath := filepath.Join(sshDirPath, file.Name())
		buf, err := os.ReadFile(filePath)
		if err != nil {
			errs = append(errs, fmt.Errorf("cannot read file %s: %w", file.Name(), err))
			continue
		}
		pk, err := ssh.ParsePrivateKey(buf)
		if err != nil {
			errs = append(errs, fmt.Errorf("cannot parse private key from file %s: %w", file.Name(), err))
			continue
		}
		signers = append(signers, pk)
	}

	return signers, errs
}

func appendAgentSigners(ctx context.Context, signers []ssh.Signer, done []func(), errs []error) ([]ssh.Signer, []func(), []error) {
	sshAuthSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAuthSock == "" {
		return signers, done, errs
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sshAuthSock)
	if err != nil {
		errs = append(errs, err)
		return signers, done, errs
	}

	type signersErr struct {
		signers []ssh.Signer
		err     error
	}
	ch := make(chan signersErr)
	go func() {
		s, e := agent.NewClient(conn).Signers()
		select {
		case ch <- signersErr{signers: s, err: e}:
		case <-ctx.Done():
		}
	}()

	select {
	case <-ctx.Done():

		_ = conn.Close()
		errs = append(errs, fmt.Errorf("ssh agent ctx: %w", ctx.Err()))

	case res := <-ch:

		signers = append(signers, res.signers...)
		done = append(done, func() { _ = conn.Close() })
		if res.err != nil {
			errs = append(errs, res.err)
		}

	}

	return signers, done, errs
}
