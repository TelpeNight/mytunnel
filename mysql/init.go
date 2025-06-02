package mysql

import (
	"github.com/TelpeNight/mytunnel/dial"
	"github.com/go-sql-driver/mysql"
)

func init() {
	mysql.RegisterDialContext("mysql+tcp", dial.DialContext)
}
