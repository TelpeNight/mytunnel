package dial

import (
	"reflect"
	"testing"

	"github.com/AlekSi/pointer"
)

func TestParseAddr(t *testing.T) {
	tests := []struct {
		name    string
		args    string
		want    Config
		wantErr bool
	}{
		{
			name:    "empty",
			args:    "",
			want:    Config{},
			wantErr: false,
		},
		{
			name: "host",
			args: "host",
			want: Config{
				Host: "host",
			},
			wantErr: false,
		},
		{
			name: "@ prefix",
			args: "@host",
			want: Config{
				Host: "host",
			},
			wantErr: true,
		},
		{
			name: "@ suffix",
			args: "user@",
			want: Config{
				Username: "user",
			},
			wantErr: true,
		},
		{
			name: "user host",
			args: "user@host",
			want: Config{
				Username: "user",
				Host:     "host",
			},
			wantErr: false,
		},
		{
			name: "user emptypass host",
			args: "user:@host",
			want: Config{
				Username: "user",
				Password: pointer.ToString(""),
				Host:     "host",
			},
			wantErr: false,
		},
		{
			name: "no user pass",
			args: ":pass@host",
			want: Config{
				Password: pointer.ToString("pass"),
				Host:     "host",
			},
			wantErr: false,
		},
		{
			name: "no user emptypass",
			args: ":@host",
			want: Config{
				Password: pointer.ToString(""),
				Host:     "host",
			},
			wantErr: false,
		},
		{
			name: "user pass host",
			args: "user:pass@host",
			want: Config{
				Username: "user",
				Host:     "host",
				Password: pointer.ToString("pass"),
			},
			wantErr: false,
		},
		{
			name: "user pass host addr",
			args: "user:pass@host/my.sock",
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
			args: "user:pass@host/",
			want: Config{
				Username: "user",
				Password: pointer.ToString("pass"),
				Host:     "host",
			},
			wantErr: true,
		},
		{
			name: "host port addr",
			args: "host:23/addr",
			want: Config{
				Host: "host",
				Port: 23,
				Net:  "unix",
				Addr: "/addr",
			},
			wantErr: false,
		},
		{
			name: "invalid port",
			args: "host:3a",
			want: Config{
				Host: "host:3a",
			},
			wantErr: true,
		},
		{
			name: "only port",
			args: ":33",
			want: Config{
				Port: 33,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAddr(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAddr() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseAddr() got = %v, want %v", got, tt.want)
			}
		})
	}
}
