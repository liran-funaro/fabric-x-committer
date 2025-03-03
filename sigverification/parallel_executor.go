package sigverification

import (
	"context"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
)

type (
	parallelExecutor struct {
		inputCh        chan *protosigverifierservice.Request
		outputSingleCh chan *protosigverifierservice.Response
		outputCh       chan []*protosigverifierservice.Response
		executor       executorFunc
		config         *ExecutorConfig
	}
	executorFunc = func(*protosigverifierservice.Request) *protosigverifierservice.Response
)

func (e *parallelExecutor) handleChannelInput(ctx context.Context) {
	chIn := channel.NewReader(ctx, e.inputCh)
	chOut := channel.NewWriter(ctx, e.outputSingleCh)
	for {
		input, ok := chIn.Read()
		if !ok {
			return
		}
		logger.Debugf("Received request %v with in worker", input.Tx.Id)
		output := e.executor(input)
		logger.Debugf("Received output from executor: %v", output)
		chOut.Write(output)
	}
}

func (e *parallelExecutor) handleCutoff(ctx context.Context) {
	var outputBuffer []*protosigverifierservice.Response
	chOut := channel.NewWriter(ctx, e.outputCh)
	cutBatch := func(size int) {
		for len(outputBuffer) >= size {
			batchSize := min(e.config.BatchSizeCutoff, len(outputBuffer))
			logger.Debugf("Cuts batch with %d/%d of the outputs.", batchSize, len(outputBuffer))
			chOut.Write(outputBuffer[:batchSize])
			outputBuffer = outputBuffer[batchSize:]
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.config.BatchTimeCutoff):
			logger.Debugf("Attempts to cut a batch (timout). (buffer size: %d)", len(outputBuffer))
			cutBatch(1)
		case output := <-e.outputSingleCh:
			logger.Debugf("Attempts to emit a batch (response). (buffer size: %d)", len(outputBuffer)+1)
			outputBuffer = append(outputBuffer, output)
			cutBatch(e.config.BatchSizeCutoff)
		}
	}
}
