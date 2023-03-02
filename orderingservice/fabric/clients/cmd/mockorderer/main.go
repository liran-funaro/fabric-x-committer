package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"google.golang.org/grpc"
)

func main() {
	tlsDir := os.Getenv("GOPATH") + "/src/github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/out/orgs/ordererOrganizations/orderer.org/orderers/raft0.orderer.org/tls"

	profile := &workload.Profile{
		Block: workload.BlockProfile{
			Count: -1,
			Size:  100,
		},
		Transaction: workload.TransactionProfile{
			SerialNumberSize: []test.DiscreteValue{{2, 1}},
			OutputSize:       []test.DiscreteValue{{1, 1}},
			SignatureType:    signature.Ecdsa,
		},
	}

	var endpoint connection.Endpoint
	connection.EndpointVar(&endpoint, "endpoint", *connection.CreateEndpoint("localhost:7051"), "Endpoint that the orderer listens to.")
	flag.Int64Var(&profile.Block.Size, "block-size", profile.Block.Size, "The size of the outgoing blocks")
	flag.Parse()

	connection.RunServerMain(&connection.ServerConfig{Endpoint: endpoint, Creds: &connection.ServerCredsConfig{tlsDir + "/server.crt", tlsDir + "/server.key"}}, func(grpcServer *grpc.Server) {
		ab.RegisterAtomicBroadcastServer(grpcServer, NewMockOrderer(profile))
	})
}

type mockOrdererImpl struct {
	once    sync.Once
	profile *workload.Profile
	blocks  chan *workload.BlockWithExpectedResult
}

func NewMockOrderer(pp *workload.Profile) *mockOrdererImpl {
	return &mockOrdererImpl{
		profile: pp,
		blocks:  make(chan *workload.BlockWithExpectedResult, 1000),
	}
}

// Broadcast receives TXs and returns ACKs
func (o *mockOrdererImpl) Broadcast(stream ab.AtomicBroadcast_BroadcastServer) error {
	fmt.Printf("Broadcast: Starting listener for new TXs. No ACKs will be returned.\n")

	//o.once.Do(func() {
	//	workload.StartBockGeneratorOnQueue(o.profile, o.blocks)
	//})

	for {
		_, err := stream.Recv()
		if err == io.EOF {
			fmt.Printf("Received EOF from %v, hangup\n", stream)
			return nil
		}
		if err != nil {
			fmt.Printf("Error reading from %v: %s\n", stream, err)
			return err
		}

		err = stream.Send(&ab.BroadcastResponse{
			Status: common.Status_SUCCESS,
		})
		if err != nil {
			return err
		}
	}
}

// Deliver receives a seek request and returns a stream of the orderered blocks
func (o *mockOrdererImpl) Deliver(stream ab.AtomicBroadcast_DeliverServer) error {
	fmt.Printf("Deliver: Starting listener for new TXs. No ACKs will be returned.\n")

	seekInfo, channelID, err := readSeekEnvelope(stream)
	if err != nil {
		return err
	}
	fmt.Printf("Received listening request for channel '%s': %v\nWe will ignore the request and send a stream anyway.", channelID, *seekInfo)

	for {
		block := mapBlock((<-o.blocks).Block)
		utils.Must(stream.Send(&ab.DeliverResponse{Type: &ab.DeliverResponse_Block{Block: block}}))
	}
}

func readSeekEnvelope(stream ab.AtomicBroadcast_DeliverServer) (*ab.SeekInfo, string, error) {
	env, err := stream.Recv()
	if err != nil {
		return nil, "", err
	}
	payload, err := protoutil.UnmarshalPayload(env.Payload)
	if err != nil {
		return nil, "", err
	}
	seekInfo := &ab.SeekInfo{}
	if err = proto.Unmarshal(payload.Data, seekInfo); err != nil {
		return nil, "", err
	}
	chdr := &common.ChannelHeader{}
	if err = proto.Unmarshal(payload.Header.ChannelHeader, chdr); err != nil {
		return nil, "", err
	}
	return seekInfo, chdr.ChannelId, nil
}

func mapBlock(block *token.Block) *common.Block {
	txs := make([][]byte, len(block.Txs))
	for i, tx := range block.Txs {
		txs[i], _ = proto.Marshal(tx)
	}
	return &common.Block{
		Header: &common.BlockHeader{
			Number:       block.Number,
			PreviousHash: []byte("prev"),
			DataHash:     []byte("data"),
		},
		Data: &common.BlockData{
			Data: txs,
		},
	}
}
