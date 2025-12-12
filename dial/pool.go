package dial

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"sync"
	"sync/atomic"
)

type (
	sshClientPool struct {
		mu sync.Mutex
		m  map[clientKey]*clientPoolEntry
	}
	clientKey struct {
		Username  string
		Password  string
		Addr      string
		KeepAlive keepAliveConfig
	}
	clientPoolEntry struct {
		done     chan struct{}
		val      *sshPooledTunnel
		refCount int64
		removed  bool

		// debug:
		accessed atomic.Bool
	}
	sshPooledTunnel struct {
		client        sshClient
		pool          *sshClientPool
		key           clientKey
		keepAliveOnce sync.Once
	}
	sshClientCtor = func(ctx context.Context) (sshClient, error)
)

func newClientPool() *sshClientPool {
	return &sshClientPool{
		m: make(map[clientKey]*clientPoolEntry),
	}
}

func (c Config) clientKey(config keepAliveConfig) clientKey {
	return clientKey{
		Username:  c.Username,
		Password:  passKey(c.Password),
		Addr:      c.sshAddr(),
		KeepAlive: config,
	}
}

func (p *sshClientPool) acquire(ctx context.Context, key clientKey, ctor sshClientCtor) (*sshPooledTunnel, error) {
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

		client, err := ctor(ctx)

		// no need to lock here
		// e is synchronized with done
		// e.val can't escape acquire or wait before done is closed, so can't be an argument for release or forget
		// and e fields are accessed in wait after done
		e.startAccess()
		if err == nil {

			e.val = &sshPooledTunnel{
				client: client,
				pool:   p,
				key:    key,
			}
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

func (e *clientPoolEntry) wait(ctx context.Context, mu *sync.Mutex) (_ *sshPooledTunnel, _ error, retry bool) {
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
	was := e.accessed.Swap(true)
	if was && clientPoolEntryRace {
		panic("mytunnel/dial: clientPoolEntry data race")
	}
}

func (e *clientPoolEntry) endAccess() {
	e.accessed.Store(false)
}

func (p *sshClientPool) release(value *sshPooledTunnel) error {
	last := p.tryRelease(value, false)
	if last {
		return value.client.Close()
	}
	return nil
}

func (p *sshClientPool) forget(value *sshPooledTunnel) {
	p.tryRelease(value, true)
	_ = value.client.Close()
}

func (p *sshClientPool) tryRelease(value *sshPooledTunnel, force bool) bool {
	if value == nil {
		panic("mytunnel/dial: tryRelease: value is nil")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	e, has := p.m[value.key]
	if !has || e.val != value {
		// ok, maybe value was forgotten and relaced in pool
		return true
	}

	e.startAccess()
	defer e.endAccess()

	e.refCount--
	if clientPoolEntryRace && e.refCount < 0 {
		panic("mytunnel/dial: clientPoolEntry refCount < 0")
	}
	if !force && e.refCount > 0 {
		return false
	}

	e.removed = true
	delete(p.m, value.key)
	return true
}

func (t *sshPooledTunnel) release() error {
	return t.pool.release(t)
}

func (t *sshPooledTunnel) forget() {
	t.pool.forget(t)
}

func passKey(password *string) string {
	if password == nil {
		return "-"
	}
	if *password == "" {
		return "*"
	}
	hash := md5.Sum([]byte(*password))
	return "-*" + hex.EncodeToString(hash[:])
}
