package dial

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"sync/atomic"
	"time"
)

var (
	useMockSshClient = false
	mockClosedCount  atomic.Uint64
)

func newMockSshClient() *mockSshClient {
	return &mockSshClient{}
}

type (
	mockSshClient struct {
		closed atomic.Bool
	}
	mochNetCon struct {
		parent *mockSshClient
		closed atomic.Bool
	}
)

func (m *mockSshClient) successfulRead() <-chan struct{} {
	return nil
}

func (m *mockSshClient) Wait() error {
	return nil
}

func (m *mockSshClient) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	return false, nil, nil
}

func (m *mockSshClient) DialContext(ctx context.Context, net string, addr string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rand.IntN(50) == 0 {
		return nil, io.EOF
	}
	return &mochNetCon{parent: m}, nil
}

func (m *mockSshClient) Close() error {
	m.closed.Store(true)
	mockClosedCount.Add(1)
	return nil
}

func (m *mochNetCon) Close() error {
	m.closed.Store(true)
	return nil
}

func (m *mochNetCon) Write(b []byte) (n int, err error) {
	if m.parent.closed.Load() {
		return 0, errors.New("access closed ssh client")
	}
	if m.closed.Load() {
		return 0, errors.New("access closed net connection")
	}
	return len(b), nil
}

func (m *mochNetCon) Read(b []byte) (n int, err error) {
	//TODO implement me
	panic("implement me")
}

func (m *mochNetCon) LocalAddr() net.Addr {
	//TODO implement me
	panic("implement me")
}

func (m *mochNetCon) RemoteAddr() net.Addr {
	//TODO implement me
	panic("implement me")
}

func (m *mochNetCon) SetDeadline(t time.Time) error {
	//TODO implement me
	panic("implement me")
}

func (m *mochNetCon) SetReadDeadline(t time.Time) error {
	//TODO implement me
	panic("implement me")
}

func (m *mochNetCon) SetWriteDeadline(t time.Time) error {
	//TODO implement me
	panic("implement me")
}
