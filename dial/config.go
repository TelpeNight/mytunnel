// Package dial provides SSH tunnel dialing. It establishes connections to
// remote network addresses by routing through an SSH server, with optional
// connection pooling and keep-alive support.
package dial

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"unicode"
)

// Config holds the parsed parameters for an SSH tunnel connection.
// Use [ParseAddr] to construct a Config from an address string.
//
// Field values are stored exactly as provided by the caller — no normalisation
// is applied. For example, Host may be a plain hostname, an IPv4 address, or
// a bracketed IPv6 literal such as "[::1]", whichever form appeared in the
// address string. [Config.Validate] checks that the fields required for a
// successful dial are present; everything else (port validity, host
// resolvability, …) is left to the underlying network stack.
type Config struct {
	// Username is the SSH login name.
	Username string
	// Password is the SSH password. If nil, only public-key auth is attempted.
	Password *string
	// Host is the SSH server hostname or IP address, stored as provided.
	// An IPv6 literal may or may not include surrounding brackets depending
	// on whether the caller included them.
	Host string
	// Port is the SSH server port as a string, stored as provided.
	// Empty means [DefaultPort] (22). Any non-numeric value is passed through
	// and will produce an error at dial time.
	Port string
	// Net is the network type of the destination ("tcp" or "unix").
	Net string
	// Addr is the address of the destination on the remote host.
	Addr string
	// Params holds optional query parameters controlling keep-alive and
	// connection behaviour. See [ParseAddr] for supported keys.
	Params url.Values
}

// DefaultPort is the SSH port used when [Config.Port] is empty.
const DefaultPort = "22"

// String serialises the config back into an address string accepted by [ParseAddr].
// The only transformation applied is bracketing a bare IPv6 host when a port is
// also present — without brackets the colon would be ambiguous as a port separator.
// All other field values are written exactly as stored.
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
	host := c.Host
	wrapIPv6 := c.Port != "" && strings.Contains(host, ":") && !strings.HasPrefix(host, "[")
	if wrapIPv6 {
		builder = append(builder, "[", host, "]")
	} else {
		builder = append(builder, host)
	}
	if c.Port != "" {
		builder = append(builder, ":", c.Port)
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
	// ErrUserRequired is returned by [Config.Validate] when Username is empty.
	ErrUserRequired = errors.New("username is required")
	// ErrHostRequired is returned by [Config.Validate] when Host is empty.
	ErrHostRequired = errors.New("host is required")
	// ErrAddrRequired is returned by [Config.Validate] when Net or Addr is empty.
	ErrAddrRequired = errors.New("addr is required")
)

// Validate checks that the fields required for a correct dial and SSH
// authentication are present: Username, Host, and the destination Net+Addr.
// It does not validate field values — port format, host resolvability, and
// similar concerns are left to the underlying dialer and SSH library, which
// will produce actionable errors if anything is wrong.
//
// [DialContext] calls Validate automatically. Call it explicitly when
// constructing a [Config] by hand rather than via [ParseAddr].
func (c Config) Validate() error {
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

// ParseAddr parses an SSH tunnel address string into a [Config].
//
// The address format is:
//
//	[username[:password]@]host[:port][/destination][?params]
//
// Parsing is purely structural: each token is split out and stored as-is,
// without validating its value. Use [Config.Validate] to check that the result
// is complete enough to dial. Malformed values (bad port, unresolvable host,
// …) are not rejected here — they will produce errors at dial time.
//
// The only error ParseAddr returns is a malformed query string in the params
// component.
//
// The destination component is resolved as follows: if it looks like
// host:port or a bare IP address, Net is set to "tcp"; otherwise it is
// treated as a Unix socket path and Net is set to "unix".
//
// The literal string "(a)" may be used anywhere in place of "@" — useful
// when embedding the address inside a MySQL DSN, where bare "@" is a
// delimiter.
//
// Supported query parameters:
//   - ServerAliveInterval — keep-alive probe interval in seconds
//   - ServerAliveCountMax — unanswered probes before closing (default 3)
//   - ServerAliveTimeout  — per-probe response timeout (default = ServerAliveInterval)
//   - ServerAliveLagMax   — debugger-pause grace period added to the timeout (default 2s)
//   - ConnMux             — "true" (default) to share one SSH client per server; "false" for a new connection per Dial
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
		result.Username, result.Password = parseUserInfo(userinfo)
	}

	hostPort, netAddrWithParams, hasSlash := strings.Cut(url_, "/")
	if hostPort != "" {
		result.Host, result.Port = parseHostPort(hostPort)
	}

	if hasSlash {
		netAddr := netAddrWithParams
		params := ""
		paramStart := strings.LastIndex(netAddrWithParams, "?")
		if paramStart >= 0 {
			netAddr, params = netAddrWithParams[:paramStart], netAddrWithParams[paramStart+1:]
		}

		if strings.TrimFunc(netAddr, pathSepAndSpace) != "" {
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

func parseUserInfo(userinfo string) (string, *string) {
	if userinfo == "" {
		return "", nil
	}
	username, password, has := strings.Cut(userinfo, ":")
	if has {
		return username, &password
	}
	return username, nil
}

// parseHostPort splits a "host" or "host:port" token.
// It uses net.SplitHostPort to detect whether a port is present, then
// substrings the original input so the host token is preserved exactly
// (e.g. "[::1]:22" yields host "[::1]", not the stripped "::1").
// If no port separator is found the whole input is returned as the host.
func parseHostPort(host string) (string, string) {
	_, port, err := net.SplitHostPort(host)
	if err != nil {
		return host, ""
	}
	h := strings.TrimSuffix(host, port)
	h = strings.TrimSuffix(h, ":")
	return h, port
}

func getAddrNet(addr string) (string, string) {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return "tcp", addr
	}
	if _, err := netip.ParseAddr(addr); err == nil {
		return "tcp", addr
	}
	return "unix", "/" + addr
}
