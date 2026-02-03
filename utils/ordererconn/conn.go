/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package ordererconn

import (
	"math/rand"

	commontypes "github.com/hyperledger/fabric-x-common/api/types"

	"github.com/hyperledger/fabric-x-committer/utils/connection"
)

// GetEndpointsForAPI returns the endpoints that matches the given API.
func GetEndpointsForAPI(endpoints []*commontypes.OrdererEndpoint, api string) []*connection.Endpoint {
	result := make([]*connection.Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		if ep.SupportsAPI(api) {
			result = append(result, &connection.Endpoint{Host: ep.Host, Port: ep.Port})
		}
	}
	// We shuffle the endpoints for load balancing.
	shuffle(result)
	return result
}

// GetEndpointsForAPIPerID returns a map of the endpoints for each ID, with the given API.
func GetEndpointsForAPIPerID(
	endpoints []*commontypes.OrdererEndpoint,
	api string,
) map[uint32][]*connection.Endpoint {
	ret := make(map[uint32][]*connection.Endpoint)
	for _, ep := range endpoints {
		if ep.SupportsAPI(api) {
			ret[ep.ID] = append(ret[ep.ID], &connection.Endpoint{Host: ep.Host, Port: ep.Port})
		}
	}
	// We shuffle the endpoints for load balancing.
	for _, ep := range ret {
		shuffle(ep)
	}
	return ret
}

func shuffle[T any](nodes []T) {
	rand.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
}
