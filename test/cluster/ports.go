package cluster

import (
	"fmt"
	"math/rand"
	"net"
	"time"
)

const (
	minPort = 49152
	maxPort = 65535
)

// FindAvailablePortRange finds a range of available ports.
func FindAvailablePortRange(numPorts int) ([]int, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	var availablePorts []int

	for len(availablePorts) < numPorts {
		randomPort := r.Intn(maxPort-minPort+1) + minPort
		if isPortAvailable(randomPort) {
			availablePorts = append(availablePorts, randomPort)
		}
	}
	if len(availablePorts) < numPorts {
		return nil, fmt.Errorf("not enough available ports found")
	}
	return availablePorts, nil
}

// Check if a port is available for use.
func isPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	conn, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
