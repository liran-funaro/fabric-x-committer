/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package adapters

import (
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/ordererdial"
	"github.com/hyperledger/fabric-x-committer/utils/serve"
)

type (
	// AdapterConfig contains all adapters configurations.
	AdapterConfig struct {
		OrdererClient     *OrdererClientConfig          `mapstructure:"orderer-client"`
		SidecarClient     *SidecarClientConfig          `mapstructure:"sidecar-client"`
		LoadGenClient     *connection.ClientConfig      `mapstructure:"loadgen-client"`
		CoordinatorClient *connection.ClientConfig      `mapstructure:"coordinator-client"`
		VCClient          *connection.MultiClientConfig `mapstructure:"vc-client"`
		VerifierClient    *connection.MultiClientConfig `mapstructure:"verifier-client"`
	}

	// OrdererClientConfig is a struct that contains the configuration for the orderer client.
	OrdererClientConfig struct {
		Orderer              ordererdial.Config `mapstructure:"orderer"`
		BroadcastParallelism int                `mapstructure:"broadcast-parallelism"`
		// SidecarClient is used to deliver status from the sidecar.
		// If omitted, we will fetch directly from the orderer.
		SidecarClient *connection.ClientConfig `mapstructure:"sidecar-client"`
	}

	// SidecarClientConfig is a struct that contains the configuration for the sidecar client.
	// OrdererServers config must correlate with the orderer endpoints of the policy.
	SidecarClientConfig struct {
		SidecarClient  *connection.ClientConfig `mapstructure:"sidecar-client"`
		OrdererServers []*serve.ServerConfig    `mapstructure:"orderer-servers"`
		// OutBlockCapacity bounds how many blocks the embedded mock orderer holds between the
		// workload submitting them and the sidecar fetching them, and so bounds the transactions
		// in flight. Zero keeps the mock orderer's own default, which is large enough that an
		// overloaded committer is absorbed rather than felt: the submission rate stays at
		// whatever was asked for, only the commit rate reveals the real drain rate, and
		// end-to-end latency grows past anything the latency histogram can represent. Set it to
		// a small multiple of the sidecar's waiting-txs-limit to make saturation observable.
		OutBlockCapacity int `mapstructure:"out-block-capacity"`
		// FastBlockPrepare moves the cost of hashing a block's data off the embedded mock orderer's
		// single block-preparing goroutine and onto the goroutine that assembles the block, and stops
		// that orderer from deep-cloning a block this adapter built for it alone.
		//
		// It changes what limits the generator, not what the committer receives: the blocks delivered
		// are identical. Worth turning on when the generator is the ceiling, which is what small
		// blocks make it -- at 500 transactions a block, preparation costs 0.75 ms and caps the
		// generator near 850 blocks a second, where the same 0.75 ms is spread over twenty times as
		// many transactions in a 10,000-transaction block.
		//
		// Off by default, so a measurement taken with it is not silently compared against one taken
		// without it.
		FastBlockPrepare bool `mapstructure:"fast-block-prepare"`
	}
)
