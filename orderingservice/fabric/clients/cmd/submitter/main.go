// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	cb "github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/bccsp/factory"
	"github.com/hyperledger/fabric/msp"
	mspmgmt "github.com/hyperledger/fabric/msp/mgmt"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.com/schollz/progressbar/v3"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients/pkg/identity"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients/pkg/tls"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"google.golang.org/grpc"
)

type broadcastClient struct {
	client    ab.AtomicBroadcast_BroadcastClient
	signer    identity.SignerSerializer
	channelID string
}

// newBroadcastClient creates a simple instance of the broadcastClient interface
func newBroadcastClient(client ab.AtomicBroadcast_BroadcastClient, channelID string, signer identity.SignerSerializer) *broadcastClient {
	return &broadcastClient{client: client, channelID: channelID, signer: signer}
}

func CreateSignedEnvelope(
	txType common.HeaderType,
	channelID string,
	signer identity.SignerSerializer,
	dataMsg proto.Message,
	msgVersion int32,
	epoch uint64,
	tlsCertHash []byte,
) (*common.Envelope, error) {
	payloadChannelHeader := protoutil.MakeChannelHeader(txType, msgVersion, channelID, epoch)
	payloadChannelHeader.TlsCertHash = tlsCertHash
	var err error
	payloadSignatureHeader := &common.SignatureHeader{}

	if signer != nil {
		payloadSignatureHeader, err = protoutil.NewSignatureHeader(signer)
		if err != nil {
			return nil, err
		}
	}

	data, err := proto.Marshal(dataMsg)
	if err != nil {
		return nil, errors.Wrap(err, "error marshaling")
	}

	paylBytes := protoutil.MarshalOrPanic(
		&common.Payload{
			Header: protoutil.MakePayloadHeader(payloadChannelHeader, payloadSignatureHeader),
			Data:   data,
		},
	)

	var sig []byte
	if signer != nil {
		sig, err = signer.Sign(paylBytes)
		if err != nil {
			return nil, err
		}
	}

	env := &common.Envelope{
		Payload:   paylBytes,
		Signature: sig,
	}

	return env, nil
}

// CreateEnvelope create an envelope WITHOUT a signature and the corresponding header
// can only be used with a patched fabric orderer
func CreateEnvelope(
	txType common.HeaderType,
	channelID string,
	signer identity.SignerSerializer,
	dataMsg proto.Message,
	msgVersion int32,
	epoch uint64,
	tlsCertHash []byte,
) (*common.Envelope, error) {
	payloadChannelHeader := protoutil.MakeChannelHeader(txType, msgVersion, channelID, epoch)
	payloadChannelHeader.TlsCertHash = tlsCertHash
	var err error

	data, err := proto.Marshal(dataMsg)
	if err != nil {
		return nil, errors.Wrap(err, "error marshaling")
	}

	paylBytes := protoutil.MarshalOrPanic(
		&common.Payload{
			Header: &cb.Header{
				ChannelHeader: protoutil.MarshalOrPanic(payloadChannelHeader),
			},
			Data: data,
		},
	)

	env := &common.Envelope{
		Payload:   paylBytes,
		Signature: nil,
	}

	return env, nil
}

func (s *broadcastClient) broadcast(transaction []byte) error {
	// TODO replace cb.ConfigValue with "our" transaction
	env, err := CreateEnvelope(cb.HeaderType_ENDORSER_TRANSACTION, s.channelID, s.signer, &cb.ConfigValue{Value: transaction}, 0, 0, nil)
	if err != nil {
		panic(err)
	}
	return s.client.Send(env)
}

func (s *broadcastClient) getAck() error {
	msg, err := s.client.Recv()
	if err != nil {
		return err
	}
	if msg.Status != cb.Status_SUCCESS {
		return fmt.Errorf("got unexpected status: %v - %s", msg.Status, msg.Info)
	}
	return nil
}

func main() {
	conf, err := localconfig.Load()
	if err != nil {
		fmt.Println("failed to load config:", err)
		os.Exit(1)
	}

	// Load local MSP
	mspConfig, err := msp.GetLocalMspConfig(conf.General.LocalMSPDir, conf.General.BCCSP, conf.General.LocalMSPID)
	if err != nil {
		fmt.Println("Failed to load MSP config:", err)
		os.Exit(0)
	}
	err = mspmgmt.GetLocalMSP(factory.GetDefault()).Setup(mspConfig)
	if err != nil { // Handle errors reading the config file
		fmt.Println("Failed to initialize local MSP:", err)
		os.Exit(0)
	}

	signer, err := mspmgmt.GetLocalMSP(factory.GetDefault()).GetDefaultSigningIdentity()
	if err != nil {
		fmt.Println("Failed to load local signing identity:", err)
		os.Exit(0)
	}

	var channelID string
	var serverAddr string
	var messages uint64
	var goroutines uint64
	var msgSize uint64
	//var bar *pb.ProgressBar

	flag.StringVar(&serverAddr, "server", fmt.Sprintf("%s:%d", conf.General.ListenAddress, conf.General.ListenPort), "The RPC server to connect to.")
	flag.StringVar(&channelID, "channelID", "mychannel", "The channel ID to broadcast to.")
	flag.Uint64Var(&messages, "messages", 1, "The number of messages to broadcast.")
	flag.Uint64Var(&goroutines, "goroutines", 1, "The number of concurrent go routines to broadcast the messages on")
	flag.Uint64Var(&msgSize, "size", 160, "The size in bytes of the data section for the payload")
	flag.Parse()

	tlsCredentials, err := tls.LoadTLSCredentials()
	if err != nil {
		fmt.Println("cannot load TLS credentials: :", err)
		os.Exit(0)
	}

	var dialOpts []grpc.DialOption
	maxMsgSize := 100 * 1024 * 1024
	dialOpts = append(dialOpts, grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(maxMsgSize),
		grpc.MaxCallSendMsgSize(maxMsgSize),
	))
	dialOpts = append(dialOpts, grpc.WithTransportCredentials(tlsCredentials))

	var connections []*grpc.ClientConn
	serverAddrs := []string{"localhost:7050", "localhost:7051", "localhost:7052"}
	for _, serverAddr := range serverAddrs {
		// let's connect to every ordering node
		conn, err := grpc.Dial(serverAddr, dialOpts...)
		defer func() {
			_ = conn.Close()
		}()
		if err != nil {
			fmt.Println("Error connecting:", err)
			return
		}

		connections = append(connections, conn)
	}

	msgsPerGo := messages / goroutines
	roundMsgs := msgsPerGo * goroutines
	if roundMsgs != messages {
		fmt.Println("Rounding messages to", roundMsgs)
	}

	bar := workload.NewProgressBar("Submitting transactions...", int64(roundMsgs), "tx")

	msgSize = 160
	msgData := make([]byte, msgSize)

	var wg sync.WaitGroup
	wg.Add(int(goroutines))
	for i := uint64(0); i < goroutines; i++ {
		go func(i uint64, pb *progressbar.ProgressBar) {
			conn := connections[i%3]
			client, err := ab.NewAtomicBroadcastClient(conn).Broadcast(context.TODO())
			if err != nil {
				fmt.Println("Error connecting:", err)
				return
			}

			s := newBroadcastClient(client, channelID, signer)
			done := make(chan (struct{}))
			go func() {
				for i := uint64(0); i < msgsPerGo; i++ {
					err = s.getAck()
					if err == nil && bar != nil {
						bar.Add(1)
					}
				}
				if err != nil {
					fmt.Printf("\nError: %v\n", err)
				}
				close(done)
			}()
			for i := uint64(0); i < msgsPerGo; i++ {
				// TODO pre-generate signed envelopes
				if err := s.broadcast(msgData); err != nil {
					panic(err)
				}
			}
			<-done
			wg.Done()
			client.CloseSend()
		}(i, bar)
	}

	wg.Wait()
	fmt.Printf("----------------------broadcast message finish-------------------------------")
}
