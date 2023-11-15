package main

import (
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

func startSendingBlocks(queue <-chan *protoblocktx.Block, send func(*protoblocktx.Block) error, c *senderTracker, logger CmdLogger, stopSenders chan any) error {
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
