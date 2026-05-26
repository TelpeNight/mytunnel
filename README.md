# mytunnel

[![Go Reference](https://pkg.go.dev/badge/github.com/TelpeNight/mytunnel.svg)](https://pkg.go.dev/github.com/TelpeNight/mytunnel)
[![Go](https://github.com/TelpeNight/mytunnel/actions/workflows/go.yml/badge.svg)](https://github.com/TelpeNight/mytunnel/actions/workflows/go.yml)
![GitHub Tag](https://img.shields.io/github/v/tag/TelpeNight/mytunnel)
[![Go Report Card](https://goreportcard.com/badge/github.com/TelpeNight/mytunnel)](https://goreportcard.com/report/github.com/TelpeNight/mytunnel)
![GitHub License](https://img.shields.io/github/license/TelpeNight/mytunnel)

Dial any network address through an SSH tunnel. Built-in MySQL support.

## Install

```
go get github.com/TelpeNight/mytunnel
```

## Usage

### Direct dial

```go
import "github.com/TelpeNight/mytunnel/dial"

conn, err := dial.DialContext(ctx, "alice@bastion.example.com/var/run/app.sock")
```

`DialContext` returns a `net.Conn` ready for use. By default, a pooled SSH
client is reused across concurrent calls to the same server — equivalent to
opening a persistent tunnel and multiplexing connections over it.

### MySQL

```go
import _ "github.com/TelpeNight/mytunnel/mysql"
```

This registers the `ssh+tunnel` network with go-mysql-driver. Example DSN:

```
db_user:db_pass@ssh+tunnel(ssh_user(a)bastion.example.com/tmp/mysql.sock?ServerAliveInterval=10)/mydb
```

Everything inside `ssh+tunnel(...)` is the tunnel address passed to
`dial.DialContext`. Use `(a)` in place of `@` inside the tunnel address —
the MySQL DSN parser treats bare `@` as a delimiter.

## Address format

```
[username[:password]@]host[:port][/destination][?params]
```

| Part | Description |
|---|---|
| `username` | SSH login name |
| `password` | SSH password (omit to use public-key auth only) |
| `host` | SSH server hostname or IP address |
| `port` | SSH server port (default: `22`) |
| `/destination` | Where to connect on the remote host. A `host:port` string or bare IP uses TCP; anything else is a Unix socket path. |
| `?params` | Optional query parameters (see below) |

`(a)` may be used in place of `@` anywhere in the address.

## Parameters

### Keep-alive

Setting these is recommended if you sometimes get SQL errors caused by stale
connections in the pool. `ServerAliveInterval` and `ServerAliveCountMax` mimic
[OpenSSH's defaults](https://man.openbsd.org/ssh_config#ServerAliveCountMax).

| Parameter | Default | Description |
|---|---|---|
| `ServerAliveInterval` | — (disabled) | Interval between keep-alive probes, in seconds |
| `ServerAliveCountMax` | `3` | Unanswered probes before the connection is closed |
| `ServerAliveTimeout` | `= ServerAliveInterval` | Timeout for a single probe. Set to the maximum latency expected in your environment. |
| `ServerAliveLagMax` | `2s` | If elapsed time exceeds `ServerAliveTimeout + ServerAliveLagMax`, the timeout is treated as a debugger pause and skipped. |

### Connection mode

| Parameter | Default | Description |
|---|---|---|
| `ConnMux` | `true` | When `true`, a pooled SSH client multiplexes all concurrent connections to the same server over a single TCP socket. Set to `false` to open a new SSH connection per `Dial` call — increases throughput, but the remote server's concurrent-connection limit applies. If you set this to `false` you may want to call `db.SetMaxOpenConns` to match the server's limit. |

## Requirements

- The SSH server host key must already be in `~/.ssh/known_hosts`.
- Auth: `~/.ssh/id_*` private keys and SSH password are supported. `SSH_AUTH_SOCK` (agent) auth is experimental.
