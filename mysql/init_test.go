package mysql

import "testing"

func Test_normalizeAdd(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		want    string
		wantErr bool
	}{
		{
			name: "empty",
			addr: "",
			want: "/127.0.0.1:3306",
		},
		{
			name: "user@host",
			addr: "user@host",
			want: "user@host/127.0.0.1:3306",
		},
		{
			name: "user@host/127.0.0.1:3305",
			addr: "user@host/127.0.0.1:3305",
			want: "user@host/127.0.0.1:3305",
		},
		{
			name: "user@host/my.sock",
			addr: "user@host/my.sock",
			want: "user@host/my.sock",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeAddr(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("normalizeAddr() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("normalizeAddr() got = %v, want %v", got, tt.want)
			}
		})
	}
}
