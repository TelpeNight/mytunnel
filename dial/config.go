package dial

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
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

const DefaultPort = 22

func (c Config) String() string {
	var builder strings.Builder
	builder.WriteString(c.Username)
	if c.Password != nil {
		builder.WriteByte(':')
		builder.WriteString(*c.Password)
	}
	if builder.Len() > 0 {
		builder.WriteByte('@')
	}
	builder.WriteString(c.Host)
	if c.Port != 0 {
		builder.WriteByte(':')
		builder.WriteString(strconv.Itoa(c.Port))
	}
	if c.Addr != "" {
		if c.Addr[0] != '/' {
			builder.WriteByte('/')
		}
		builder.WriteString(c.Addr)
	}
	return builder.String()
}

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
		result.Net, result.Addr = getAddrNet(netAddr)
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

func getAddrNet(addr string) (string, string) {
	host, _, errHostPort := net.SplitHostPort(addr)
	if errHostPort == nil {
		_, errIpAddr := netip.ParseAddr(host)
		if errIpAddr == nil {
			return "tcp", addr
		}
	}
	_, errIpAddr := netip.ParseAddr(addr)
	if errIpAddr == nil {
		return "tcp", addr
	}
	return "unix", "/" + addr
}
