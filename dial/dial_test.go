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
	critPanic = true
	mockClosedCount.Store(0)

	var dialCount atomic.Int64
	var addrs = make([]string, 0, runtime.GOMAXPROCS(0)+2)
	for i := 0; i < runtime.GOMAXPROCS(0)+2; i++ {
		addrs = append(addrs, fmt.Sprintf("user@host%d/my.sock", i))
	}

	var ctx = context.Background()
	dial := func() {
		dialCount.Add(1)
		smallDelay()
		addr := addrs[rand.IntN(len(addrs))]
		con, err := DialContext(ctx, addr)
		if err != nil {
			b.Error(err)
		}
		_, err = con.Write([]byte("hello"))
		if err != nil {
			b.Error(err)
		}
		workDelay()
		_, err = con.Write([]byte("world"))
		if err != nil {
			b.Error(err)
		}
		err = con.Close()
		if err != nil {
			b.Error(err)
		}
		err = con.Close()
		if err != nil {
			b.Error(err)
		}
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			dial()
		}
	})

	b.Logf("dial count: %d, closed: %d", dialCount.Load(), mockClosedCount.Load())
}

const delayScale = int64(time.Millisecond)

func smallDelay() {
	if rand.IntN(2) == 0 {
		return
	}
	dur := time.Duration(rand.Int64N(delayScale))
	time.Sleep(dur)
}

func workDelay() {
	dur := 5 * time.Duration(rand.Int64N(delayScale))
	time.Sleep(dur)
}
