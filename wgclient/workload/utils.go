package workload

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
)

const queueSize = 100

func NewProgressBar(description string, max int64, unit string) *progressbar.ProgressBar {
	return progressbar.NewOptions64(max,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionFullWidth(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString(unit),
		progressbar.OptionShowCount(),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetDescription(fmt.Sprintf("%s", description)),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))
}

func NewBlockProgressBar(description string, max int64) *progressbar.ProgressBar {
	return NewProgressBar(description, max, "blocks")
}

func PrintStats(numTx, numBlocks, received int64, elapsedPushed, totalElapsed time.Duration) {
	fmt.Printf("\n### Results ###\n")
	fmt.Printf("Sent: %d tx (%d blocks) pushed in %s\n", numTx, numBlocks, elapsedPushed)
	fmt.Printf("Received: %d statuses in %s\n", received, totalElapsed)
	fmt.Printf("TPS: %.2f\n", float64(numTx)/totalElapsed.Seconds())
}

func GetByteWorkload(path string) ([]byte, chan []byte, *Profile) {
	key, err := GetKey(path)
	utils.Must(err)

	f, err := os.Open(path)
	utils.Must(err)
	// TODO can we improve here by tweaking the buffered reader size?
	reader := bufio.NewReader(f)

	// load profile from block file
	pp := ReadProfileFromBlockFile(reader)

	// reading blocks into buffered channel
	dQueue := make(chan []byte, queueSize)
	go ByteReader(reader, dQueue, nil)

	return key, dQueue, pp
}

func GetBlockWorkload(path string) ([]byte, chan *BlockWithExpectedResult, *Profile) {
	key, err := GetKey(path)
	utils.Must(err)

	f, err := os.Open(path)
	utils.Must(err)
	// TODO can we improve here by tweaking the buffered reader size?
	reader := bufio.NewReader(f)

	// load profile from block file
	pp := ReadProfileFromBlockFile(reader)

	// reading blocks into buffered channel
	bQueue := make(chan *BlockWithExpectedResult, queueSize)
	go BlockReader(reader, bQueue, nil)

	return key, bQueue, pp
}

func GetEvents(path string) (chan *Event, *Profile) {

	f, err := os.Open(path)
	utils.Must(err)
	//reader := bufio.NewReader(f)

	// load profile from block file
	//pp := ReadProfileFromBlockFile(reader)

	// reading blocks into buffered channel

	scanner := bufio.NewScanner(f)
	eQueue := make(chan *Event, queueSize)
	go func() {
		for scanner.Scan() {
			e := &Event{}
			err = json.Unmarshal(scanner.Bytes(), e)
			utils.Must(err)
			eQueue <- e
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintln(os.Stderr, "reading standard input:", err)
		}
		close(eQueue)
	}()

	return eQueue, nil
}

func GetKey(path string) ([]byte, error) {
	return ioutil.ReadFile(path + ".pem")
}
