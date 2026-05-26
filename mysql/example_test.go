package mysql_test

import (
	"database/sql"

	_ "github.com/TelpeNight/mytunnel/mysql"
)

func Example() {
	// Importing the package registers the "ssh+tunnel" network with
	// go-mysql-driver. Use (a) in place of @ inside the tunnel address to
	// avoid conflicts with the MySQL DSN parser.
	dsn := "db_user:db_pass@ssh+tunnel(ssh_user(a)bastion.example.com/tmp/mysql.sock?ServerAliveInterval=10)/mydb"
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// db is ready to use — connections are established lazily.
	_ = db
}
