package streamhandler

import (
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("streamhandler")

type (
	Input  = *sigverification.RequestBatch
	Output = *sigverification.ResponseBatch
	Stream = sigverification.Verifier_StartStreamServer
)

// StreamHandler is an adapter between streams and a pair of pub/sub methods
type StreamHandler struct {
	// InputSubscriber determines what to do with the incoming requests from the client
	InputSubscriber func(Input)
	// OutputPublisher produces outgoing responses to send to the client
	OutputPublisher func() Output
}

func New(inputSubscriber func(Input), outputPublisher func() Output) *StreamHandler {
	return &StreamHandler{
		InputSubscriber: inputSubscriber,
		OutputPublisher: outputPublisher,
	}
}

func (s *StreamHandler) HandleStream(stream Stream) {
	if s.InputSubscriber == nil || s.OutputPublisher == nil {
		panic("input or output handler missing")
	}
	go s.handleOutputs(stream)
	s.handleInputs(stream)
}

func (s *StreamHandler) handleInputs(stream Stream) {
	for {
		// TODO: Add cancel
		input, rpcErr := stream.Recv()
		if rpcErr != nil {
			if err := connection.FilterStreamRPCError(rpcErr); err != nil {
				logger.Errorf("failed to serve request: %v", err)
			}
			return
		}
		logger.Debugf("Received input from client with %v requests", len(input.Requests))

		s.InputSubscriber(input)
	}
}

func (s *StreamHandler) handleOutputs(stream Stream) {
	for {
		output := s.OutputPublisher()
		logger.Debugf("Received output: %v", output)
		err := stream.Send(output)
		if err != nil {
			logger.Infof("Failed to send output to client: %v", err)
			// TODO: Replace panics with error handling
			return
		}
		logger.Debugf("Forwarded output to client.")
	}
}
