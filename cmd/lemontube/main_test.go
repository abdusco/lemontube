package main

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBrowserURL(t *testing.T) {
	for _, tt := range []struct {
		name string
		addr *net.TCPAddr
		want string
	}{
		{name: "wildcard IPv4", addr: &net.TCPAddr{IP: net.IPv4zero, Port: 8081}, want: "http://localhost:8081"},
		{name: "wildcard IPv6", addr: &net.TCPAddr{IP: net.IPv6unspecified, Port: 8081}, want: "http://localhost:8081"},
		{name: "IPv4", addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}, want: "http://127.0.0.1:1234"},
		{name: "IPv6", addr: &net.TCPAddr{IP: net.IPv6loopback, Port: 1234}, want: "http://[::1]:1234"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, browserURL(tt.addr))
		})
	}
}
