package cmd

import "testing"

func TestLoopbackAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:7654", true},
		{"localhost:7654", true},
		{"[::1]:7654", true},
		{"0.0.0.0:7654", false},
		{"192.168.1.20:7654", false},
		{"bad-address", false},
	}
	for _, test := range tests {
		if got := loopbackAddr(test.addr); got != test.want {
			t.Errorf("loopbackAddr(%q)=%v want %v", test.addr, got, test.want)
		}
	}
}
