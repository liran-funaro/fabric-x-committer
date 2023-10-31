package ordererclient

import (
	"context"
	errors2 "errors"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const rcvChanCapacity = 100

type broadcastResponse = struct {
	*ab.BroadcastResponse
	error
}

type BroadcastStream struct {
	streams []ab.AtomicBroadcast_BroadcastClient
	stop    chan any
	rcv     chan *broadcastResponse
}

func NewBroadcastStream(connections []*grpc.ClientConn) (ab.AtomicBroadcast_BroadcastClient, error) {
	streams := make([]ab.AtomicBroadcast_BroadcastClient, len(connections))
	errs := make([]error, len(connections))
	for i, c := range connections {
		streams[i], errs[i] = ab.NewAtomicBroadcastClient(c).Broadcast(context.TODO())
	}
	if err := errors2.Join(errs...); err != nil {
		return nil, errors.Wrap(err, "failed to create broadcast stream")
	}

	stop := make(chan any)

	rcv := make(chan *broadcastResponse, len(streams)*rcvChanCapacity)
	for _, stream := range streams {
		go func(stream ab.AtomicBroadcast_BroadcastClient) {
			for {
				select {
				case <-stop:
					return
				default:
				}
				r, e := stream.Recv()
				rcv <- &broadcastResponse{r, e}
			}
		}(stream)
	}

	return &BroadcastStream{streams: streams, stop: stop, rcv: rcv}, nil
}
func (s *BroadcastStream) Header() (metadata.MD, error) { panic("unimplemented") }
func (s *BroadcastStream) Trailer() metadata.MD         { panic("unimplemented") }

func (s *BroadcastStream) broadcast(fn func(client ab.AtomicBroadcast_BroadcastClient) error) error {
	errs := make([]error, len(s.streams))
	for _, stream := range s.streams {
		errs = append(errs, fn(stream))
	}
	return errors2.Join(errs...)
}

func (s *BroadcastStream) CloseSend() error {
	defer func() {
		s.stop <- struct{}{}
	}()
	return s.broadcast(func(stream ab.AtomicBroadcast_BroadcastClient) error {
		return stream.CloseSend()
	})
}
func (s *BroadcastStream) Context() context.Context { panic("unimplemented") }
func (s *BroadcastStream) SendMsg(m interface{}) error {
	return s.broadcast(func(stream ab.AtomicBroadcast_BroadcastClient) error {
		return stream.SendMsg(m)
	})
}
func (s *BroadcastStream) RecvMsg(m interface{}) error { panic("unimplemented") }
func (s *BroadcastStream) Send(m *common.Envelope) error {
	return s.broadcast(func(stream ab.AtomicBroadcast_BroadcastClient) error {
		return stream.Send(m)
	})
}
func (s *BroadcastStream) Recv() (*ab.BroadcastResponse, error) {
	rcv := <-s.rcv
	return rcv.BroadcastResponse, rcv.error
}
