package main

import (
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

func StartSending(queue <-chan *protoblocktx.Block, send func(*protoblocktx.Block) error, c SenderTracker, logger CmdLogger, stopSenders chan any) error {
	for {
		select {
		case <-stopSender:
			logger("stopping sender")
			return nil
		default:
		}

		block := <-queue
		if err := send(block); err != nil {
			return errors.Wrap(err, "failed sending")
		}

		c.OnSendBlock(block)
	}
}
