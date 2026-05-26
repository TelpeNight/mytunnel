package dial_test

import (
	"context"
	"fmt"

	"github.com/TelpeNight/mytunnel/dial"
)

func ExampleDialContext() {
	// Dial a Unix socket on the remote host through SSH.
	// Keep-alive probes run every 30 seconds; the connection is closed after
	// 3 unanswered probes.
	conn, err := dial.DialContext(context.Background(),
		"alice@bastion.example.com/var/run/app.sock?ServerAliveInterval=30&ServerAliveCountMax=3")
	if err != nil {
		// handle error
		return
	}
	defer conn.Close()

	// conn is a plain net.Conn ready for use.
	_ = conn
}

func ExampleParseAddr() {
	config, err := dial.ParseAddr("alice@bastion.example.com/tmp/mysql.sock")
	if err != nil {
		panic(err)
	}
	fmt.Println(config.Host)
	fmt.Println(config.Net)
	fmt.Println(config.Addr)
	// Output:
	// bastion.example.com
	// unix
	// /tmp/mysql.sock
}
