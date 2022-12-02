package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/filecoin-project/mir"
	"github.com/filecoin-project/mir/pkg/events"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	cb "github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/common/util"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
)

func NewServer(address string, node *mir.Node, b chan *cb.Block) *Server {

	grpcServer, err := NewGRPCServer(address)
	if err != nil {
		panic(err)
	}

	server := &Server{
		GRPCServer: grpcServer,
		node:       node,
		blockChan:  b,
	}

	ab.RegisterAtomicBroadcastServer(grpcServer.Server(), server)
	if err := grpcServer.Start(); err != nil {
		panic(err)
	}

	return server
}

type Server struct {
	GRPCServer *GRPCServer
	node       *mir.Node
	blockChan  chan *cb.Block
}

// Broadcast receives a stream of messages from a client for ordering
func (s *Server) Broadcast(srv ab.AtomicBroadcast_BroadcastServer) error {
	addr := util.ExtractRemoteAddress(srv.Context())

	defer func(addr string) {
		fmt.Printf("connection closed with %s\n", addr)
	}(addr)

	for {
		envelope, err := srv.Recv()
		if err == io.EOF {
			fmt.Printf("Received EOF from %s, hangup\n", addr)
			return nil
		}
		if err != nil {
			fmt.Printf("Error reading from %s: %s\n", addr, err)
			return err
		}

		status, _ := s.broadcast(srv.Context(), envelope)

		resp := &ab.BroadcastResponse{
			Status: status,
		}

		//fmt.Printf("send resp\n")
		err = srv.Send(resp)
		if status != cb.Status_SUCCESS {
			return err
		}
		if err != nil {
			return err
		}
	}

}

func (s *Server) broadcast(ctx context.Context, envelope *cb.Envelope) (cb.Status, error) {

	_, chdr, shdr, err := parseEnvelope(ctx, envelope)
	if err != nil {
		return cb.Status_BAD_REQUEST, nil
	}
	seqNo, _ := strconv.ParseUint(chdr.TxId, 0, 64)
	serializedEnvelope, _ := protoutil.Marshal(envelope)

	req := &requestpb.Request{
		ClientId: shdr.String(),
		ReqNo:    seqNo,
		Data:     serializedEnvelope,
	}

	//fmt.Printf("req: %s\n", req)

	// Submit the request to the Node.
	srErr := s.node.InjectEvents(context.TODO(), events.ListOf(events.NewClientRequests(
		"mempool",
		[]*requestpb.Request{req},
	)))
	if srErr != nil {
		fmt.Printf("error: %v", srErr)
		return cb.Status_INTERNAL_SERVER_ERROR, nil
	}

	return cb.Status_SUCCESS, nil
}

func parseEnvelope(ctx context.Context, envelope *cb.Envelope) (*cb.Payload, *cb.ChannelHeader, *cb.SignatureHeader, error) {
	payload, err := protoutil.UnmarshalPayload(envelope.Payload)
	if err != nil {
		return nil, nil, nil, err
	}

	if payload.Header == nil {
		return nil, nil, nil, errors.New("envelope has no header")
	}

	chdr, err := protoutil.UnmarshalChannelHeader(payload.Header.ChannelHeader)
	if err != nil {
		return nil, nil, nil, err
	}

	shdr, err := protoutil.UnmarshalSignatureHeader(payload.Header.SignatureHeader)
	if err != nil {
		return nil, nil, nil, err
	}

	return payload, chdr, shdr, nil
}

// Deliver sends a stream of blocks to a client after ordering
func (s *Server) Deliver(srv ab.AtomicBroadcast_DeliverServer) error {
	addr := util.ExtractRemoteAddress(srv.Context())
	fmt.Printf("New deliver client connected from %s\n", addr)

	defer func(addr string) {
		fmt.Printf("connection closed with %s\n", addr)
	}(addr)

	for {
		envelope, err := srv.Recv()
		if err == io.EOF {
			fmt.Printf("Received EOF from %s, hangup\n", addr)
			return nil
		}
		if err != nil {
			fmt.Printf("Error reading from %s: %s\n", addr, err)
			return err
		}

		_ = envelope

		//block := &cb.Block{
		//	Header: &cb.BlockHeader{
		//		Number:       0,
		//		PreviousHash: nil,
		//		DataHash:     nil,
		//	},
		//	Data: &cb.BlockData{
		//		Data: [][]byte{[]byte("hallo")},
		//	},
		//	Metadata: &cb.BlockMetadata{
		//		Metadata: nil,
		//	},
		//}

		for block := range s.blockChan {
			resp := &ab.DeliverResponse{
				Type: &ab.DeliverResponse_Block{Block: block},
			}

			err = srv.Send(resp)
			if err != nil {
				return err
			}
		}

		return nil
	}
}

func (s *Server) Stop() {
	s.GRPCServer.Stop()
}

type GRPCServer struct {
	// Listen address for the server specified as hostname:port
	address string
	// Listener for handling network requests
	listener net.Listener
	// GRPC server
	server *grpc.Server
	// Certificate presented by the server for TLS communication
	// stored as an atomic reference
	serverCertificate atomic.Value
	// lock to protect concurrent access to append / remove
	lock *sync.Mutex
	// TLS configuration used by the grpc server
	//tls *TLSConfig
	// Server for gRPC Health Check Protocol.
	healthServer *health.Server
}

func NewGRPCServer(address string) (*GRPCServer, error) {
	if address == "" {
		return nil, errors.New("missing address parameter")
	}
	// create our listener
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}

	grpcServer := &GRPCServer{
		address:  listener.Addr().String(),
		listener: listener,
		lock:     &sync.Mutex{},
	}

	var serverOpts []grpc.ServerOption

	creds, err := loadTLSCredentials()
	if err != nil {
		panic(err)
	}
	serverOpts = append(serverOpts, grpc.Creds(creds))

	// todo check Server opts
	grpcServer.server = grpc.NewServer(serverOpts...)

	return grpcServer, nil
}

func loadTLSCredentials() (credentials.TransportCredentials, error) {
	// Load server's certificate and private key
	serverCert, err := tls.LoadX509KeyPair("testdata/server.crt", "testdata/server.key")
	if err != nil {
		return nil, err
	}

	// Create the credentials and return it
	config := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.NoClientCert,
	}

	return credentials.NewTLS(config), nil
}

// Start starts the underlying grpc.Server
func (gServer *GRPCServer) Start() error {
	go func() {
		gServer.server.Serve(gServer.listener)
	}()
	return nil
}

// Stop stops the underlying grpc.Server
func (gServer *GRPCServer) Stop() {
	gServer.server.Stop()
}

func (gServer *GRPCServer) Server() *grpc.Server {
	return gServer.server
}
