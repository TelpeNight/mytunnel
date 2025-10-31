package dial

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

type Config struct {
	Username string
	Password *string
	Host     string
	Port     int
	Net      string
	Addr     string
	Params   url.Values
}

const DefaultPort = 22

func (c Config) String() string {
	var builder = make([]string, 0, 11)
	if c.Username != "" {
		builder = append(builder, c.Username)
	}
	if c.Password != nil {
		builder = append(builder, ":", *c.Password)
	}
	if len(builder) > 0 {
		builder = append(builder, "@")
	}
	builder = append(builder, c.Host)
	if c.Port != 0 {
		builder = append(builder, ":", strconv.Itoa(c.Port))
	}
	if c.Addr != "" {
		if c.Addr[0] != '/' {
			builder = append(builder, "/")
		}
		builder = append(builder, c.Addr)
	}
	if len(c.Params) > 0 {
		builder = append(builder, "?")
		builder = append(builder, c.Params.Encode())
	}
	return strings.Join(builder, "")
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
	userinfo, url_, hasUserInfo := strings.Cut(addr, "@")
	if !hasUserInfo {
		url_ = addr
	}

	var errs []error
	if hasUserInfo {
		if url_ == "" {
			errs = append(errs, ErrHostRequired)
		}
		var userInfoErr error
		result.Username, result.Password, userInfoErr = parseUserInfo(userinfo)
		if userInfoErr != nil {
			errs = append(errs, userInfoErr)
		}
	}

	hostPort, netAddrWithParams, hasSlash := strings.Cut(url_, "/")
	if hostPort != "" {
		var hostPortErr error
		result.Host, result.Port, hostPortErr = parseHostPort(hostPort)
		if hostPortErr != nil {
			errs = append(errs, hostPortErr)
		}
	}

	if hasSlash {
		netAddr := netAddrWithParams
		params := ""
		paramStart := strings.LastIndex(netAddrWithParams, "?")
		if paramStart >= 0 {
			netAddr, params = netAddrWithParams[:paramStart], netAddrWithParams[paramStart+1:]
		}

		if strings.TrimFunc(netAddr, pathSepAndSpace) == "" {
			errs = append(errs, ErrAddrRequired)
		} else {
			result.Net, result.Addr = getAddrNet(netAddr)
		}

		if params != "" {
			var paramsErr error
			result.Params, paramsErr = url.ParseQuery(params)
			if paramsErr != nil {
				errs = append(errs, paramsErr)
			}
		}
	}

	return result, wrapErr(errors.Join(errs...))
}

func pathSepAndSpace(r rune) bool {
	switch r {
	case '/', '\\':
		return true
	}
	return unicode.IsSpace(r)
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
