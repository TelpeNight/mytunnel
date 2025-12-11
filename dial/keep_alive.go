package dial

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func makeKeepAliveConfig(values url.Values) keepAliveConfig {
	var result = keepAliveConfig{
		serverAliveCountMax: -1,
		serverAliveInterval: -1,
		serverAliveTimeout:  -1,
		serverAliveLagMax:   -1,
	}
	for k, v := range values {
		switch {
		case strings.EqualFold(k, "ServerAliveInterval"):
			result.serverAliveInterval = kaParse(k, v, time.Second)
		case strings.EqualFold(k, "ServerAliveCountMax"):
			result.serverAliveCountMax = kaParse(k, v, 1)
		case strings.EqualFold(k, "ServerAliveTimeout"):
			result.serverAliveTimeout = kaParse(k, v, time.Second)
		case strings.EqualFold(k, "ServerAliveLagMax"):
			result.serverAliveLagMax = kaParse(k, v, time.Second)
		}
	}
	if !result.keepAlive() {
		return keepAliveConfig{}
	}
	if result.serverAliveTimeout <= 0 {
		result.serverAliveTimeout = result.serverAliveInterval
	}
	if result.serverAliveCountMax < 0 {
		result.serverAliveCountMax = serverAliveCountMax
	}
	if result.serverAliveLagMax < 0 {
		result.serverAliveLagMax = serverAliveLagMax
	}
	return result
}

type intType interface {
	~int64 | ~int
}

func kaParse[Int intType](key string, vals []string, units Int) Int {
	if len(vals) == 0 {
		return -1
	}
	if len(vals) > 1 {
		logger().Error(fmt.Sprintf("mytunnel/dial: multiple values for %s, skipping keep alive param", key))
		return -1
	}
	res, err := strconv.ParseUint(vals[0], 10, 64)
	if err == nil && res > math.MaxInt {
		err = fmt.Errorf("value %s > MaxInt", vals[0])
	}
	if err != nil {
		logger().Error(fmt.Sprintf("mytunnel/dial: invalid value for %s, skipping keep alive param", key), "err", err.Error())
		return -1
	}
	return Int(res) * units
}

func keepAlive(cli sshClient, config keepAliveConfig) {
	if config.keepAlive() {
		go keepAliveLoop(cli, config)
	}
}

type keepAliveConfig struct {
	serverAliveCountMax int
	serverAliveInterval time.Duration
	serverAliveTimeout  time.Duration
	serverAliveLagMax   time.Duration
}

func (c keepAliveConfig) keepAlive() bool {
	return c.serverAliveInterval > 0
}

const (
	serverAliveCountMax = 3
	serverAliveLagMax   = 2 * time.Second
)

func keepAliveLoop(cli sshClient, config keepAliveConfig) {
	ticker := time.NewTicker(config.serverAliveInterval)
	defer ticker.Stop()
	done := make(chan struct{})
	defer close(done)
	keepAliveReq := make(chan struct{})
	defer close(keepAliveReq)
	keepAliveResp := keepAliveSingleFlight(cli, keepAliveReq, done)

	waitDone := make(chan struct{})
	go func() {
		_ = cli.Wait()
		close(waitDone)
	}()

	serverAliveCounter := config.serverAliveCountMax
	hadTimeout := false
	for {
		if hadTimeout {
			serverAliveCounter--
		} else {
			serverAliveCounter = config.serverAliveCountMax
		}
		hadTimeout = false

		select {
		case <-waitDone:
			// should be already closed, but make sure
			_ = cli.Close()
			return
		case <-ticker.C:
			start := time.Now()
			err := sendKeepAliveRequest(cli, keepAliveReq, keepAliveResp, config.serverAliveTimeout, config.serverAliveLagMax)
			if err == nil {
				ticker.Reset(config.serverAliveInterval)
				continue
			}
			if err == io.EOF {
				// io.EOF is emitted by client.SendRequest, when the client is closed
				// should be already closed, but make sure
				_ = cli.Close()
				return
			}

			//goland:noinspection GoDirectComparisonOfErrors
			hadTimeout = err == errKeepAliveTimeout
			if hadTimeout && serverAliveCounter > 0 {
				continue
			}

			_ = cli.Close()
			logger().Debug("mytunnel/dial: keepAlive", "err", err.Error(), "took", time.Since(start))
			return
		case <-cli.successfulRead():
			ticker.Reset(config.serverAliveInterval)
		}
	}
}

func keepAliveSingleFlight(client sshClient, req <-chan struct{}, done <-chan struct{}) <-chan error {
	resp := make(chan error)
	go func() {
		defer close(resp)
		for range req {
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			select {
			case resp <- err:
			case <-done:
				return
			}
		}
	}()
	return resp
}

var errKeepAliveTimeout = errors.New("keep alive timeout")

var logEveryKeepAliveRequest = false

func sendKeepAliveRequest(client sshClient, req chan<- struct{}, resp <-chan error, timeout, lag time.Duration) (err error) {
	select {
	case err := <-resp:
		// maybe there is a result of a previous timed-out request
		if err != nil {
			return err
		}
	default:
	}

	if logEveryKeepAliveRequest {
		logStart := time.Now()
		logger().Debug("mytunnel/dial: stating keep alive", "client", client, "timeout", timeout, "lag", lag)
		defer func() {
			logger().Debug("mytunnel/dial: finished keep alive", "client", client, "took", time.Since(logStart), "timeout", timeout, "lag", lag, "err", err)
		}()
	}

	start := time.Now()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	handleTimeout := func() error {
		took := time.Since(start)
		if took >= timeout+lag {
			logger().Debug("mytunnel/dial: sendKeepAliveRequest: seems to be paused by debugger (or some other lag), skipping timeout", "took", took, "timeout", timeout, "lag", lag)
			return nil
		}
		return errKeepAliveTimeout
	}

	select {
	case req <- struct{}{}:
		select {
		case <-timer.C:
			return handleTimeout()
		case err := <-resp:
			return err
		case <-client.successfulRead():
			// in-flight request will be consumed at the beginning of [sendKeepAliveRequest]
			return nil
		}
	case <-timer.C:
		return handleTimeout()
	case err := <-resp:
		// maybe there is a result of a previous timed-out out request
		// it was completed after the timer has started, so we can consume the result
		return err
	case <-client.successfulRead():
		return nil
	}
}
