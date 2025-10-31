package dial

import (
	"net"
)

// https://github.com/golang/go/issues/57531

type netConn struct {
	net.Conn
	//stickyWriteErr atomic.Pointer[error]
}

//func (c *netConn) Write(b []byte) (n int, err error) {
//	n, err = c.Conn.Write(b)
//	if err != nil {
//		c.stickyWriteErr.CompareAndSwap(nil, pointer.To(err))
//	}
//	return
//}
//
//func (c *netConn) getNetWriteError() error {
//	err := c.stickyWriteErr.Load()
//	if err != nil {
//		return *err
//	}
//	return nil
//}
