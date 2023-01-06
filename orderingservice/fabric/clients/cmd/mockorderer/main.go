package main

import (
	"flag"
	"fmt"
	"os"

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
	tlsDir := os.Getenv("GOPATH") + "/src/github.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/out/orgs/ordererOrganizations/orderer.org/orderers/raft0.orderer.org/tls"

	profile := &workload.Profile{
		Block: workload.BlockProfile{
			Count: -1,
			Size:  100,
		},
		Transaction: workload.TransactionProfile{
			Size:          []test.DiscreteValue{{2, 1}},
			SignatureType: signature.Ecdsa,
		},
	}

	var endpoint connection.Endpoint
	connection.EndpointVar(&endpoint, "endpoint", *connection.CreateEndpoint("localhost:7050"), "Endpoint that the orderer listens to.")
	flag.Int64Var(&profile.Block.Size, "block-size", profile.Block.Size, "The size of the outgoing blocks")
	flag.Parse()

	creds := connection.LoadCreds(tlsDir+"/server.crt", tlsDir+"/server.key")
	connection.RunServerMain(&connection.ServerConfig{Endpoint: endpoint, Opts: []grpc.ServerOption{creds}}, func(grpcServer *grpc.Server) {
		ab.RegisterAtomicBroadcastServer(grpcServer, NewMockOrderer(profile))
	})
}

type mockOrdererImpl struct {
	blocks chan *workload.BlockWithExpectedResult
}

func NewMockOrderer(pp *workload.Profile) *mockOrdererImpl {
	_, blocks := workload.StartBlockGenerator(pp)
	return &mockOrdererImpl{blocks: blocks}
}

//Broadcast receives TXs and returns ACKs
func (o *mockOrdererImpl) Broadcast(stream ab.AtomicBroadcast_BroadcastServer) error {
	fmt.Printf("Starting listener for new TXs. No ACKs will be returned.\n")

	for {
		_, _ = stream.Recv()
	}
}

//Deliver receives a seek request and returns a stream of the orderered blocks
func (o *mockOrdererImpl) Deliver(stream ab.AtomicBroadcast_DeliverServer) error {

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
