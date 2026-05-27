package dial

import (
	"cmp"
	"reflect"
	"testing"

	"github.com/AlekSi/pointer"
)

func TestParseAddr(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		want    Config
		wantStr string
		wantErr bool
	}{
		{
			name:    "empty",
			addr:    "",
			want:    Config{},
			wantErr: false,
		},
		{
			name: "host",
			addr: "host",
			want: Config{
				Host: "host",
			},
			wantErr: false,
		},
		{
			name: "@ prefix",
			addr: "@host",
			want: Config{
				Host: "host",
			},
			wantStr: "host",
			wantErr: false,
		},
		{
			name: "@ suffix",
			addr: "user@",
			want: Config{
				Username: "user",
			},
			wantErr: false,
		},
		{
			name: "user host",
			addr: "user@host",
			want: Config{
				Username: "user",
				Host:     "host",
			},
			wantErr: false,
		},
		{
			name: "user emptypass host",
			addr: "user:@host",
			want: Config{
				Username: "user",
				Password: pointer.ToString(""),
				Host:     "host",
			},
			wantErr: false,
		},
		{
			name: "no user pass",
			addr: ":pass@host",
			want: Config{
				Password: pointer.ToString("pass"),
				Host:     "host",
			},
			wantErr: false,
		},
		{
			name: "no user emptypass",
			addr: ":@host",
			want: Config{
				Password: pointer.ToString(""),
				Host:     "host",
			},
			wantErr: false,
		},
		{
			name: "user pass host",
			addr: "user:pass@host",
			want: Config{
				Username: "user",
				Host:     "host",
				Password: pointer.ToString("pass"),
			},
			wantErr: false,
		},
		{
			name: "user pass host addr",
			addr: "user:pass@host/my.sock",
			want: Config{
				Username: "user",
				Host:     "host",
				Password: pointer.ToString("pass"),
				Net:      "unix",
				Addr:     "/my.sock",
			},
			wantErr: false,
		},
		{
			name: "host empty addr",
			addr: "user:pass@host/",
			want: Config{
				Username: "user",
				Password: pointer.ToString("pass"),
				Host:     "host",
			},
			wantStr: "user:pass@host",
			wantErr: false,
		},
		{
			name: "host port addr",
			addr: "host:23/addr",
			want: Config{
				Host: "host",
				Port: "23",
				Net:  "unix",
				Addr: "/addr",
			},
			wantErr: false,
		},
		{
			name: "invalid port",
			addr: "host:3a",
			want: Config{
				Host: "host",
				Port: "3a",
			},
			wantErr: false,
		},
		{
			name: "only port",
			addr: ":33",
			want: Config{
				Port: "33",
			},
			wantErr: false,
		},
		{
			name: "tcp",
			addr: "/127.0.0.1:3305",
			want: Config{
				Net:  "tcp",
				Addr: "127.0.0.1:3305",
			},
			wantErr: false,
		},
		{
			name: "tcp hostname",
			addr: "/mysql-server:3306",
			want: Config{
				Net:  "tcp",
				Addr: "mysql-server:3306",
			},
			wantErr: false,
		},
		{
			name: "tcp ipv6",
			addr: "/::1",
			want: Config{
				Net:  "tcp",
				Addr: "::1",
			},
			wantErr: false,
		},
		{
			name: "tcp ipv6 port",
			addr: "/[::1]:3306",
			want: Config{
				Net:  "tcp",
				Addr: "[::1]:3306",
			},
			wantErr: false,
		},
		{
			name: "ssh host ipv6 default port",
			addr: "user@[::1]/my.sock",
			want: Config{
				Username: "user",
				Host:     "[::1]",
				Net:      "unix",
				Addr:     "/my.sock",
			},
			wantErr: false,
		},
		{
			name: "ssh host ipv6 explicit port",
			addr: "user@[::1]:2222/my.sock",
			want: Config{
				Username: "user",
				Host:     "[::1]",
				Port:     "2222",
				Net:      "unix",
				Addr:     "/my.sock",
			},
			wantErr: false,
		},
		{
			name: "ssh host bare ipv6 default port",
			addr: "user@::1/my.sock",
			want: Config{
				Username: "user",
				Host:     "::1",
				Net:      "unix",
				Addr:     "/my.sock",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAddr(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAddr() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseAddr() got = %v, want %v", got, tt.want)
			}
			if err == nil {
				want := cmp.Or(tt.wantStr, tt.addr)
				if want != got.String() {
					t.Errorf("Config.String() = %v, want %v", got.String(), want)
				}
			}
		})
	}
}
