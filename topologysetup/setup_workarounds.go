package topologysetup

import (
	"runtime"
	"strings"

	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/api"
	"github.com/hyperledger-labs/fabric-smart-client/integration/nwo/fabric"
)

type platformFactory struct {
	rootDir      string
	peerPorts    map[NodeID]NodeConfig
	ordererPorts map[NodeID]NodeConfig
}

func NewCustomPlatformFactory(rootDir string, peerPorts, ordererPorts map[NodeID]NodeConfig) *platformFactory {
	return &platformFactory{rootDir, peerPorts, ordererPorts}
}

func (f platformFactory) Name() string {
	return "fabric"
}

func (f platformFactory) New(registry api.Context, t api.Topology, builder api.Builder) api.Platform {
	p := fabric.NewPlatform(&EnhancedRegistry{registry, f.rootDir, f.peerPorts, f.ordererPorts}, t, builder)
	//p.Network.AddExtension(tss.NewExtension(p))
	return p
}

//TODO: Temporary workaround until we refactor NWO
type EnhancedRegistry struct {
	api.Context
	rootDir      string
	peerPorts    map[NodeID]NodeConfig
	ordererPorts map[NodeID]NodeConfig
}

func (r *EnhancedRegistry) PortsByPeerID(_ string, id NodeID) api.Ports {
	return r.peerPorts[id].Ports
}

func (r *EnhancedRegistry) PortsByOrdererID(_ string, id NodeID) api.Ports {
	return r.ordererPorts[id].Ports
}

func (r *EnhancedRegistry) HostByPeerID(_ string, id NodeID) string {
	return r.peerPorts[id].Host
}

func (r *EnhancedRegistry) HostByOrdererID(_ string, id NodeID) string {
	return r.ordererPorts[id].Host
}

func (r *EnhancedRegistry) RootDir() string {
	if isInTemplate(1) {
		return r.rootDir
	}
	return r.Context.RootDir()
}

func isInTemplate(skip int) bool {
	rpc := make([]uintptr, 20)
	n := runtime.Callers(skip+1, rpc[:])
	if n < 1 {
		return false
	}
	frames := runtime.CallersFrames(rpc)
	for {
		frame, more := frames.Next()
		if !more {
			return false
		}
		if strings.Contains(frame.Function, "(*Template).Execute") {
			return true
		}
	}
}
