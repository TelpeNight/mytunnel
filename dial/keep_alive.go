package dial

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"time"
)

//TODO config to client key

func (t *sshPooledTunnel) keepAlive(config url.Values) {
	t.keepAliveOnce.Do(func() {
		go keepAlive(t, config)
	})
}

func keepAlive(tunn *sshPooledTunnel, config url.Values) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()

	waitDone := make(chan struct{})
	go func() {
		_ = tunn.client.Wait()
		close(waitDone)
	}()

	for {
		select {
		case <-waitDone:
			tunn.forget()
			return
		case <-t.C:
			err := sendKeepAliveRequest(tunn.client, 5*time.Second)
			if err != nil {
				tunn.forget()
				if !errors.Is(err, io.EOF) {
					slog.Debug("mytunnel/dial: keepAlive", "err", err.Error())
				}
				return
			}
		}
	}
}

const keepAliveLag = time.Second * 5

func sendKeepAliveRequest(client sshClient, timeout time.Duration) error {
	start := time.Now()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	done := make(chan error, 1)
	go func() {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		done <- err
	}()

	select {
	case <-timer.C:
		took := time.Since(start)
		if took >= timeout+keepAliveLag {
			slog.Debug("mytunnel/dial: sendKeepAliveRequest: seems to be paused by debugger (or other lag), skipping timeout")
			return nil
		}
		return context.DeadlineExceeded
	case err := <-done:
		return err
	}
}
