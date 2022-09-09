package workload

import (
	"encoding/binary"
	"io"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils"

	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"google.golang.org/protobuf/proto"
)

// BlockWriter write all block available in bQueue to a given writer
func BlockWriter(writer io.Writer, bQueue <-chan *token.Block, cntFunc func()) {
	for block := range bQueue {
		serializedBlock := MarshallBlock(block)
		WriteToFile(writer, serializedBlock)

		// call optional counter function
		if cntFunc != nil {
			cntFunc()
		}
	}
}

func BlockReader(reader io.Reader, bQueue chan *token.Block, cntFunc func()) {
	// read
	for data := Next(reader); data != nil; data = Next(reader) {
		// serialize
		block := UnmarshallBlock(data)

		// write to queue
		bQueue <- block

		if cntFunc != nil {
			cntFunc()
		}
	}
	close(bQueue)
}

func ByteReader(reader io.Reader, bQueue chan []byte, cntFunc func()) {
	// read
	for data := Next(reader); data != nil; data = Next(reader) {
		// write to queue
		bQueue <- data

		if cntFunc != nil {
			cntFunc()
		}
	}
	close(bQueue)
}

func MarshallBlock(block *token.Block) []byte {
	serializedBlock, err := proto.Marshal(block)
	utils.Must(err)
	return serializedBlock
}

func UnmarshallBlock(data []byte) *token.Block {
	block := &token.Block{}
	err := proto.Unmarshal(data, block)
	utils.Must(err)
	return block
}

func Next(reader io.Reader) []byte {
	buf := make([]byte, 4)
	if _, err := io.ReadAtLeast(reader, buf, 4); err != nil {
		if err == io.EOF {
			return nil
		}
		utils.Must(err)
	}
	itemSize := binary.LittleEndian.Uint32(buf)

	item := make([]byte, itemSize)
	if _, err := io.ReadAtLeast(reader, item, int(itemSize)); err != nil {
		if err == io.EOF {
			return nil
		}
		utils.Must(err)
	}

	return item
}

func WriteToFile(writer io.Writer, data []byte) {
	// write size of data
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(len(data)))

	_, err := writer.Write(buf)
	utils.Must(err)

	// write serialized proto
	_, err = writer.Write(data)
	utils.Must(err)
}
