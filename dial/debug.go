package dial

import "context"

// global mutex should be closed inside this function

func critNoDuplicateKey(ctx context.Context, conf Config, tun *sshTunnel) {
	_, has := tunnelCache.cache[tun.key]
	if has {
		crit()
	}
}

func critClosedTunnelInCache(ctx context.Context, conf Config, tun *sshTunnel) {
	crit()
}

func critTunnelNegUsers(ctx context.Context, conf Config, tun *sshTunnel, users int64) {
	crit()
}

func critUsersEqZero(t *sshTunnel, users int64) {
	if users != 0 {
		crit()
	}
}

func critNotClosed(t *sshTunnel) {
	if t.closed {
		crit()
	}
}

func critInCache(t *sshTunnel) {
	_, has := tunnelCache.cache[t.key]
	if !has {
		crit()
	}
}

var critPanic bool

func crit() {
	if critPanic {
		panic("mytunnel/dial crit")
	}
	println("mytunnel/dial crit")
}
