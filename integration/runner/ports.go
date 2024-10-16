package runner

import (
	"math/rand"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	minPort = 49152
	maxPort = 65535
)

// findAvailablePortRange finds a range of available ports.
func findAvailablePortRange(t *testing.T, numPorts int) []int {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	var availablePorts []int

	for len(availablePorts) < numPorts {
		randomPort := r.Intn(maxPort-minPort+1) + minPort
		if isPortAvailable(randomPort) && !slices.Contains(availablePorts, randomPort) {
			availablePorts = append(availablePorts, randomPort)
		}
	}
	require.GreaterOrEqual(t, len(availablePorts), numPorts, "not enough available ports")
	return availablePorts
}

// Check if a port is available for use.
func isPortAvailable(port int) bool {
	addr := makeLocalListenAddress(port)
	conn, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
