package dial

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Config struct {
	Username string
	Password *string
	Host     string
	Port     int
	Net      string
	Addr     string
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

func (c Config) sshAddr() string {
	port := c.Port
	if port == 0 {
		port = DefaultPort
	}
	return fmt.Sprintf("%s:%d", c.Host, port)
}

const DefaultPort = 22

var (
	ErrUserRequired = errors.New("username is required")
	ErrHostRequired = errors.New("host is required")
	ErrAddrRequired = errors.New("addr is required")
)

func ParseAddr(addr string) (Config, error) {
	var result Config
	if addr == "" {
		return result, nil
	}

	addr = strings.ReplaceAll(addr, "(a)", "@")
	userinfo, url, hasUserInfo := strings.Cut(addr, "@")
	if !hasUserInfo {
		url = addr
	}
	hostPort, netAddr, hasNetAddr := strings.Cut(url, "/")

	var errs []error
	if hasUserInfo {
		if url == "" {
			errs = append(errs, ErrHostRequired)
		}
		var userInfoErr error
		result.Username, result.Password, userInfoErr = parseUserInfo(userinfo)
		if userInfoErr != nil {
			errs = append(errs, userInfoErr)
		}
	}

	if hostPort != "" {
		var hostPortErr error
		result.Host, result.Port, hostPortErr = parseHostPort(hostPort)
		if hostPortErr != nil {
			errs = append(errs, hostPortErr)
		}
	}

	if hasNetAddr && (netAddr == "" || netAddr == "/") {
		errs = append(errs, ErrAddrRequired)
	} else if netAddr != "" {
		var netErr error
		result.Net, result.Addr, netErr = getAddrNet(netAddr)
		if netErr != nil {
			errs = append(errs, netErr)
		}
	}

	return result, wrapErr(errors.Join(errs...))
}

func parseUserInfo(userinfo string) (string, *string, error) {
	if userinfo == "" {
		return "", nil, ErrUserRequired
	}
	username, password, has := strings.Cut(userinfo, ":")
	if has {
		return username, &password, nil
	}
	return username, nil, nil
}

func parseHostPort(host string) (string, int, error) {
	portIndex := strings.LastIndex(host, ":")
	if portIndex == -1 {
		return host, 0, nil
	} else {
		portStr := host[portIndex+1:]
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return host, 0, fmt.Errorf("invalid port %q: %w", portStr, err)
		}
		host = host[:portIndex]
		if host == "" {
			return host, port, ErrHostRequired
		}
		return host, port, nil
	}
}

func getAddrNet(addr string) (string, string, error) {
	return "unix", "/" + addr, nil
}
