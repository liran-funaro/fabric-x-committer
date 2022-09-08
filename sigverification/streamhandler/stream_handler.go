package streamhandler

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("streamhandler")

type Input = *sigverification.RequestBatch
type Output = *sigverification.ResponseBatch
type Stream = sigverification.Verifier_StartStreamServer

//StreamHandler is an adapter between streams and a pair of pub/sub methods
type StreamHandler struct {
	//InputSubscriber determines what to do with the incoming requests from the client
	InputSubscriber func(Input)
	//OutputPublisher produces outgoing responses to send to the client
	OutputPublisher func() Output
}

func OfExecutor(executor parallelexecutor.ParallelExecutor) *StreamHandler {
	return &StreamHandler{
		InputSubscriber: func(batch *sigverification.RequestBatch) {
			executor.Submit(batch.Requests)
		},
		OutputPublisher: func() Output {
			return &sigverification.ResponseBatch{Responses: <-executor.Outputs()}
		},
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
		//TODO: Add cancel
		input, err := stream.Recv()
		if err != nil {
			logger.Infof("failed to serve request: %v", err)
			return
		}
		logger.Debugf("Received input from client: %v", input)

		s.InputSubscriber(input)
	}
}

func (s *StreamHandler) handleOutputs(stream Stream) {
	for {
		output := s.OutputPublisher()
		logger.Debugf("Received output: %v", output)
		err := stream.Send(output)
		if err != nil {
			logger.Infof("Failed to send output to client.")
			//TODO: Replace panics with error handling
			return
		}
		logger.Debugf("Forwarded output to client.")
	}
}
