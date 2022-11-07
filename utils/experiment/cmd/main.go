package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/experiment"
)

const (
	rateInterval = 30 * time.Second
	resultJoiner = ","
)

// -prometheus-endpoint=tokentestbed16.sl.cloud9.ibm.com:9094 -sampling-times=1667579016,1667579026,1667579116 -output=./tmp.txt
func main() {
	prometheusHost := flag.String("prometheus-endpoint", "", "The host that runs prometheus and serves the metrics.")
	unixTimestamps := connection.SliceFlag("sampling-times", []string{}, "When to take the samples (UNIX timestamp). Must be in the past")
	outputFilePath := flag.String("output", "", "The path to the output file.")
	flag.Parse()

	prometheusEndpoint := connection.CreateEndpoint(*prometheusHost)
	if prometheusEndpoint == nil {
		panic("invalid prometheus endpoint")
	}
	if len(*unixTimestamps) == 0 {
		panic("no timestamps passed")
	}
	samplingTimes := make([]time.Time, len(*unixTimestamps))
	for i, unixTimestamp := range *unixTimestamps {
		if unixTimestamp == "" {
			continue //TODO: Remove this
		}
		num, err := strconv.ParseInt(unixTimestamp, 10, 64)
		if err != nil {
			panic(err)
		}
		samplingTimes[i] = time.Unix(num, 0)
		if samplingTimes[i].After(time.Now()) {
			panic("future sampling not allowed")
		}
	}

	reader := experiment.NewResultReader(connection.CreateEndpoint(*prometheusHost), rateInterval)

	lines := make([]string, len(samplingTimes)+1)
	lines[0] = strings.Join(reader.ReadHeaders(), resultJoiner)
	for i, samplingTime := range samplingTimes {
		lines[i+1] = convertToString(reader.ReadExperimentResults(samplingTime))
	}

	utils.Must(utils.WriteFile(*outputFilePath, []byte(strings.Join(lines, "\n"))))
}

func convertToString(results []float64) string {
	return strings.Trim(strings.Join(strings.Fields(fmt.Sprint(results)), resultJoiner), "[]")
}
