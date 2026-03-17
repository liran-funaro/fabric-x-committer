/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package ordererconn

import (
	"math/rand/v2"

	"github.com/hyperledger/fabric-x-common/common/channelconfig"

	"github.com/hyperledger/fabric-x-committer/utils/connection"
)

type (
	// ConnectionMaterial contains the connection material for an orderer.
	ConnectionMaterial struct {
		Joint *connection.ClientMaterial
		PerID map[uint32]*connection.ClientMaterial
	}

	// MaterialParameters are the parameters to create connection material.
	MaterialParameters struct {
		TLS   connection.TLSMaterials
		Retry *connection.RetryProfile
		API   string
	}
)

// OrdererConnectionMaterial returns the connection material given the config block material and the given parameters.
func OrdererConnectionMaterial(m *channelconfig.ConfigBlockMaterial, p MaterialParameters) *ConnectionMaterial {
	res := &ConnectionMaterial{
		Joint: &connection.ClientMaterial{
			TLS:   p.TLS,
			Retry: p.Retry,
		},
		PerID: make(map[uint32]*connection.ClientMaterial),
	}
	for _, org := range m.OrdererOrganizations {
		var caCerts [][]byte
		if len(p.TLS.Mode) > 0 && p.TLS.Mode != connection.NoneTLSMode && len(org.CACerts) > 0 {
			caCerts = org.CACerts
		}

		endpoints := make([]*connection.Endpoint, 0, len(org.Endpoints))
		for _, ep := range org.Endpoints {
			if !ep.SupportsAPI(p.API) {
				continue
			}
			perID, ok := res.PerID[ep.ID]
			if !ok {
				perID = &connection.ClientMaterial{
					TLS:   p.TLS,
					Retry: p.Retry,
				}
				perID.TLS.CACerts = append(perID.TLS.CACerts, caCerts...)
				res.PerID[ep.ID] = perID
			}
			connEp := &connection.Endpoint{Host: ep.Host, Port: ep.Port}

			endpoints = append(endpoints, connEp)
			perID.Endpoints = append(perID.Endpoints, connEp)
		}
		if len(endpoints) == 0 {
			continue
		}
		res.Joint.Endpoints = append(res.Joint.Endpoints, endpoints...)
		res.Joint.TLS.CACerts = append(res.Joint.TLS.CACerts, caCerts...)
	}

	// We shuffle the endpoints for load balancing.
	shuffle(res.Joint.Endpoints)
	for _, mat := range res.PerID {
		shuffle(mat.Endpoints)
	}
	return res
}

func shuffle[T any](nodes []T) {
	rand.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
}
