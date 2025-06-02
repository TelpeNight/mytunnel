package dial

import (
	"context"
	"errors"
	"net"
)

func DialContext(ctx context.Context, addr string) (net.Conn, error) {
	return nil, errors.New("implement me")
}
