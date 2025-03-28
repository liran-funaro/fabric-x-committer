package runner

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type portAllocator struct {
	listeners []net.Listener
}

// allocatePorts finds a range of available ports.
func (p *portAllocator) allocatePorts(t *testing.T, count int) []*connection.Endpoint {
	t.Helper()
	endpoints := make([]*connection.Endpoint, count)
	for i := range endpoints {
		s := connection.NewLocalHostServer()
		listener, err := s.Listener()
		require.NoError(t, err)
		p.listeners = append(p.listeners, listener)
		endpoints[i] = &s.Endpoint
	}
	return endpoints
}

// close releases the ports to be used for their intended purpose.
func (p *portAllocator) close() {
	connection.CloseConnectionsLog(p.listeners...)
	p.listeners = nil
}
