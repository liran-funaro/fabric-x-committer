package workload

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

func NewProgressBar(description string, max int64) *progressbar.ProgressBar {
	return progressbar.NewOptions64(max,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionFullWidth(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("blocks"),
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

func PrintStats(pp *Profile, elapsed time.Duration) {
	fmt.Printf("\nResult: %d tx (%d blocks)  pushed in %s\n", pp.Block.Count*pp.Block.Size, pp.Block.Count, elapsed)
	fmt.Printf("tps: %f\n", float64(pp.Block.Count*pp.Block.Size)/elapsed.Seconds())
}

func GetWorkload(path string) ([]byte, chan []byte, *Profile) {
	key, err := GetKey(path)
	utils.Must(err)

	f, err := os.Open(path)
	utils.Must(err)
	// TODO can we improve here by tweaking the buffered reader size?
	reader := bufio.NewReader(f)

	// load profile from block file
	pp := ReadProfileFromBlockFile(reader)

	// reading blocks into buffered channel
	queueSize := 1000
	dQueue := make(chan []byte, queueSize)
	go ByteReader(reader, dQueue, func() {})

	return key, dQueue, pp
}

func GetKey(path string) ([]byte, error) {
	return ioutil.ReadFile(path + ".pem")
}
