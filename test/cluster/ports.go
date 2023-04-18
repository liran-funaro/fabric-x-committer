package cluster

import (
	"fmt"
	"os"
)

// TestPortRange represents a port range
type TestPortRange int

const (
	basePort = 20000
	// Node denotes a single sigverifier or shardserver
	portsPerNode = 10
	// Suite denotes either a group of sigverifier or shardserver
	portsPerSuite = 50
)

const (
	CoordinatorBasePort TestPortRange = basePort + (portsPerSuite*portsPerNode)*iota
	SigVerifierBasePort
	ShardServerBasePort
)

// On linux, the default ephemeral port range is 32768-60999 and can be
// allocated by the system for the client side of TCP connections or when
// programs explicitly request one. Given linux is our default CI system,
// we want to try avoid ports in that range.
func (t TestPortRange) StartPort() int {
	const startEphemeral, endEphemeral = 32768, 60999

	port := int(t)
	if port >= startEphemeral-portsPerNode && port <= endEphemeral-portsPerNode {
		fmt.Fprintf(os.Stderr, "WARNING: port %d is part of the default ephemeral port range on linux", port)
	}
	return port
}
