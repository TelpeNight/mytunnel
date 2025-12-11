package dial

import (
	"net"
)

type netConn struct {
	net.Conn
	readCh chan struct{}
}

func (c *netConn) Read(b []byte) (n int, err error) {
	n, err = c.Conn.Read(b)
	if c.readCh != nil && n > 0 && err == nil {
		select {
		case c.readCh <- struct{}{}:
		default:
		}
	}
	return
}

var logConnectionLifeCircle = false

func (c *netConn) onOpen() {
	if logConnectionLifeCircle {
		logger().Debug("mytunnel/dial: connection opened", "conn", c)
	}
}

func (c *netConn) onFail(err error) {
	if logConnectionLifeCircle {
		logger().Debug("mytunnel/dial: connection failed", "conn", c, "err", err)
	}
}

func (c *netConn) Close() error {
	err := c.Conn.Close()
	if logConnectionLifeCircle {
		logger().Debug("mytunnel/dial: connection closed", "conn", c, "err", err)
	}
	return err
}
