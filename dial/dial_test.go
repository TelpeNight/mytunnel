package dial

import (
	"context"
	"fmt"
	"math/rand/v2"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkDialContext(b *testing.B) {
	useMockSshClient = true
	mockClosedCount.Store(0)

	var dialCount atomic.Int64
	var addrs = make([]string, 0, runtime.GOMAXPROCS(0)+2)
	for i := 0; i < runtime.GOMAXPROCS(0)+2; i++ {
		addrs = append(addrs, fmt.Sprintf("user@host%d/my.sock", i))
	}

	dial := func() {
		dialCount.Add(1)
		smallDelay()
		ctx, cancel := maybeTimeoutCtx()
		defer cancel()
		addr := addrs[rand.N(len(addrs))]
		con, err := DialContext(ctx, addr)
		if err != nil {
			return
		}
		_, _ = con.Write([]byte("hello"))
		workDelay()
		_, _ = con.Write([]byte("world"))
		_ = con.Close()
		_ = con.Close()
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			dial()
		}
	})

	b.Logf("dial count: %d, closed: %d", dialCount.Load(), mockClosedCount.Load())
}

func maybeTimeoutCtx() (context.Context, func()) {
	if rand.IntN(2) == 0 {
		return context.Background(), func() {}
	}
	const (
		maxTimeout = 100 * time.Microsecond
		expired    = 10 * time.Microsecond
	)
	timeout := rand.N(maxTimeout+expired) - expired
	return context.WithTimeout(context.Background(), timeout)
}

const delayScale = time.Millisecond

func smallDelay() {
	if rand.IntN(2) == 0 {
		return
	}
	dur := rand.N(delayScale)
	time.Sleep(dur)
}

func workDelay() {
	dur := 5 * rand.N(delayScale)
	time.Sleep(dur)
}
