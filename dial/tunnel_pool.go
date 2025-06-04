package dial

import (
	"context"
	"net"
	"sync"
)

type (
	sshClientPool struct {
		mu sync.Mutex
		m  map[clientKey]*clientPoolEntry
	}
	clientKey struct {
		Username string
		Host     string
		Port     int
	}
	clientPoolEntry struct {
		done     chan struct{}
		val      sshClient
		refCount int64
		removed  bool
		//accessed atomic.Bool
	}
	sshClient interface {
		DialContext(ctx context.Context, net string, addr string) (net.Conn, error)
		Close() error
	}

	sshClientCtor = func(ctx context.Context) (sshClient, error)
)

func (p *sshClientPool) acquire(ctx context.Context, key clientKey, ctor sshClientCtor) (sshClient, error) {
	for {
		p.mu.Lock()
		if e, has := p.m[key]; has {
			p.mu.Unlock()

			client, err, retry := e.wait(ctx, &p.mu)
			if err != nil {
				return nil, err
			}
			if retry {
				continue
			}
			return client, nil
		}

		e := &clientPoolEntry{
			done: make(chan struct{}),
		}
		p.m[key] = e // !has, so this is the unique e by key
		p.mu.Unlock()

		var err error
		e.val, err = ctor(ctx)

		// no need to lock here
		// e is synchronized with done
		// e.val can't escape acquire or wait before done is closed, so can't be an argument for release or forget
		// and e fields are accessed in wait after done
		e.startAccess()
		if err == nil {
			e.refCount++
		} else {
			e.removed = true
			p.mu.Lock()
			delete(p.m, key)
			p.mu.Unlock()
		}
		e.endAccess()

		close(e.done)
		// from here waiters can proceed

		return e.val, err
	}
}

func (e *clientPoolEntry) wait(ctx context.Context, mu *sync.Mutex) (_ sshClient, _ error, retry bool) {
	if err := ctx.Err(); err != nil {
		return nil, err, false
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err(), false
	case <-e.done:
		mu.Lock()
		defer mu.Unlock()
		e.startAccess()
		defer e.endAccess()
		if e.removed {
			return nil, nil, true
		}
		e.refCount++
		return e.val, nil, false
	}
}

var clientPoolEntryRace = false

func (e *clientPoolEntry) startAccess() {
	//was := e.accessed.Swap(true)
	//if was && clientPoolEntryRace {
	//	panic("clientPoolEntry data race")
	//}
}

func (e *clientPoolEntry) endAccess() {
	//e.accessed.Store(false)
}

func (p *sshClientPool) release(key clientKey, value sshClient) error {
	last := p.tryRelease(key, value)
	if last {
		return value.Close()
	}
	return nil
}

func (p *sshClientPool) tryRelease(key clientKey, value sshClient) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	e, has := p.m[key]
	if !has || e.val != value {
		// ok, maybe value was forgotten and relaced in pool
		return false
	}

	e.startAccess()
	defer e.endAccess()

	e.refCount--
	if e.refCount > 0 {
		return false
	}

	e.removed = true
	delete(p.m, key)
	return true
}

func (p *sshClientPool) forget(key clientKey, value sshClient) {
	p.tryForget(key, value)
	_ = value.Close()
}

func (p *sshClientPool) tryForget(key clientKey, value sshClient) {
	p.mu.Lock()
	defer p.mu.Unlock()

	e, has := p.m[key]
	if !has || e.val != value {
		// ok, maybe value was forgotten and relaced in pool
		return
	}

	e.startAccess()
	defer e.endAccess()

	e.removed = true
	delete(p.m, key)
	return
}
