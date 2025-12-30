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

`ServerAliveTimeout` - extra param, that sets keep alive request timeout. By default, equals to `ServerAliveInterval`.
Prefer to set it to maximum latency you expect for alive connection in you environment.

`ServerAliveLagMax`. While debugging, one can get ServerAliveTimeouts, caused by debugger pauses.
To prevent this, library have extra check, that real time interval between internal timer's start and end is not greater than requested timer's interval.
If `time.Since(start) >= ServerAliveTimeout+ServerAliveTimeout`, this timeout is skipped. Default value is 2s.

`ConnMux`. By default, the library uses ssh client pool. One client can multiplex several connections.
This is equivalent to default behavior, when you open ssh tunnel between local and remote socket, and establish several connection to local one.
This can support big number of simultaneous connections to a remote socket.
But note, that in this case client ↔ server connection is single TCP socket, which can limit throughput.
By setting `ConnMux` to false, one can enable new client ↔ server TCP connection per Dial call. But in this scenario, remote ssh server can support limited number of simultaneous connections.
You may want to `SetMaxOpenConns` on you DB to match your remote server limits. Otherwise, you may get ssh handshake errors with large connection pool.  

### Mysql

Supported by registering `ssh+tunnel` net. Example DSN:

`db_user:db_pass@ssh+tunnel(ssh_user(a)example.com/tmp/my.sock?ServerAliveInterval=10)/database?param=value`

Everything inside `ssh+tunnel(...)` will be passed to `dial.DialContext`.
`(a)` symbol is a workaround for default mysql driver DSN parser. Extra `@` breaks it.

### Current restrictions

* Supports only `~/.ssh/id_*` and password authentications. SSH_AUTH_SOCK auth is experimental
* Requires host to be already added to `~/.ssh/known_hosts`
* No ENV variables to customize yet