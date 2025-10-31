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
