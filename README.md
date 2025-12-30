# mytunnel

A simple way to dial through ssh. Supports **mysql** from the box. Just

```go
import _ "github.com/TelpeNight/mytunnel/mysql"
```

### Dial address

`username[:password]@example.com[:port]/path/to/unix.sock[?params...]`

Default port is `22`

`@` can be replaced with `(a)` (see below)

### Params

`ServerAliveInterval` and `ServerAliveCountMax` mimic [default OpenSSH behavior](https://man.openbsd.org/ssh_config#ServerAliveCountMax).
It is recommented to set these values, if you sometimes get SQL error, caused by dead connections in the pool.

`ServerAliveTimeout` - an extra param, that sets keep alive request timeout. By default, equals to `ServerAliveInterval`.
Prefer to set it to the maximum latency expected in your environment.

`ServerAliveLagMax`. While debugging, you can get ServerAliveTimeouts, caused by debugger pauses.
To prevent this, we check if `time.Since(start) >= ServerAliveTimeout+ServerAliveLagMax`. In this case timeout would be skipped.
Default value is 2s.

`ConnMux`. By default, the library uses ssh client pool. One client can multiplex several connections.
This is equivalent to default behavior, when you open an ssh tunnel between local and remote sockets and establish several connection to a local one.
This can support big number of simultaneous connections to a remote socket.
But note that in this case client ↔ server connection is a single TCP socket, which can limit throughput.
By setting `ConnMux` to false, you can enable a new client ↔ server TCP connection per a Dial call. But in this scenario, a remote ssh server may support limited number of simultaneous connections.
You may want to `SetMaxOpenConns` on you DB to match your remote server limits. Otherwise, you may get ssh handshake errors with large connection pool.  

### Mysql

Supported by registering `ssh+tunnel` net. Example DSN:

`db_user:db_pass@ssh+tunnel(ssh_user(a)example.com/tmp/my.sock?ServerAliveInterval=10)/database?param=value`

Everything inside `ssh+tunnel(...)` will be passed to `dial.DialContext`.
`(a)` symbol is a workaround for the default mysql driver DSN parser. Extra `@` breaks it.

### Current restrictions

* Supports only `~/.ssh/id_*` and password authentications. SSH_AUTH_SOCK auth is experimental
* Requires host to be already added to `~/.ssh/known_hosts`
* No ENV variables to customize yet