package client

import (
	"net"
	"time"
)

// OverlayNetwork impl the overlay network for ws
func OverlayNetwork(address string, timeout time.Duration) (net.Conn, error) {
	return DialConn(address, timeout)
}
