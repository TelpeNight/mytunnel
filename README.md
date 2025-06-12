# mytunnel

A simple way to dial through ssh. Supports **mysql** from the box. Just

```go
import _ "github.com/TelpeNight/mytunnel/mysql"
```

### Dial address

`username[:password]@example.com[:port]/path/to/unix.sock`

Default port is `22`

`@` can be replaced with `(a)` (see below)

### Mysql

Supported by registering `ssh+tunnel` net. Example DSN:

`db_user:db_pass@ssh+tunnel(ssh_user(a)example.com/tmp/my.sock)/database?param=value`

Everything inside `ssh+tunnel(...)` will be passed to `dial.DialContext`.
`(a)` symbol is a workaround for default mysql driver DSN parser. Extra `@` breaks it.

### Current restrictions

* Supports only `~/.ssh/id_rsa` and password authentications
* Requires host to be already added to `~/.ssh/known_hosts`
* No ENV variables to customize yet