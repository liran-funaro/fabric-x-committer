package v1

import (
	"bufio"
	"encoding/json"
	"io"
	"os"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload"
	"google.golang.org/protobuf/proto"
)

const queueSize = 100

func BlockReaderV1(reader io.Reader, bQueue chan *BlockWithExpectedResult, cntFunc func()) {
	// read
	for data := workload.Next(reader); data != nil; data = workload.Next(reader) {
		// serialize
		block := &BlockWithExpectedResult{}
		err := proto.Unmarshal(data, block)
		utils.Must(err)

		// write to queue
		bQueue <- block

		if cntFunc != nil {
			cntFunc()
		}
	}
	close(bQueue)
}

func GetBlockWorkload(path string) ([]byte, chan *BlockWithExpectedResult, *workload.Profile) {
	key, err := workload.GetKey(path)
	utils.Must(err)

	f, err := os.Open(path)
	utils.Must(err)
	// TODO can we improve here by tweaking the buffered reader size?
	reader := bufio.NewReader(f)

	// load profile from block file
	pp := ReadProfileFromBlockFile(reader)

	// reading blocks into buffered channel
	bQueue := make(chan *BlockWithExpectedResult, queueSize)
	go BlockReaderV1(reader, bQueue, nil)

	return key, bQueue, pp
}

func ReadProfileFromBlockFile(reader io.Reader) *workload.Profile {
	data := workload.Next(reader)
	pp := &workload.Profile{}
	err := json.Unmarshal(data, pp)
	utils.Must(err)

	//PrintProfile(pp)

	return pp
}
