// Package mysql registers an "ssh+tunnel" network type with go-mysql-driver,
// allowing MySQL connections to be routed through an SSH tunnel.
//
// Import this package for its side effect:
//
//	import _ "github.com/TelpeNight/mytunnel/mysql"
//
// Then use "ssh+tunnel" as the network in your DSN. Because the MySQL DSN
// parser treats "@" as a delimiter, use "(a)" in place of "@" inside the
// tunnel address:
//
//	db_user:db_pass@ssh+tunnel(ssh_user(a)bastion.example.com/tmp/mysql.sock?ServerAliveInterval=10)/mydb
//
// Everything inside the parentheses is passed verbatim to [dial.DialContext].
// When the destination address is omitted, package-level defaults match
// go-sql-driver/mysql: 127.0.0.1:3306 for TCP and /tmp/mysql.sock for Unix.
package mysql

import (
	"context"
	"net"

	"github.com/TelpeNight/mytunnel/dial"
	"github.com/go-sql-driver/mysql"
)

func init() {
	mysql.RegisterDialContext("ssh+tunnel", dialContext)
}

func dialContext(ctx context.Context, addr string) (net.Conn, error) {
	normalized, err := normalizeAddr(addr)
	if err != nil {
		return nil, err
	}
	return dial.DialContext(ctx, normalized)
}

func normalizeAddr(addr string) (string, error) {
	config, err := dial.ParseAddr(addr)
	if err != nil {
		return "", err
	}
	if config.Net == "" {
		config.Net = "tcp"
	}
	if config.Addr == "" {
		switch config.Net {
		case "tcp":
			config.Addr = "127.0.0.1:3306"
		case "unix":
			config.Addr = "/tmp/mysql.sock"
		}
	}
	return config.String(), nil
}
