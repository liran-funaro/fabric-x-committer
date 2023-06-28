package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/experiment"
)

const resultJoiner = ","

// -metrics-endpoint=tokentestbed16.sl.cloud9.ibm.com:9094 -sampling-times=1667579016,1667579026,1667579116 -output=./tmp.txt
func main() {
	clientHost := flag.String("client-endpoint", "", "The client endpoint.")
	prometheusHost := flag.String("metrics-endpoint", "", "The host that runs prometheus and serves the metrics.")
	trackerFilePath := flag.String("input", "", "The path to the tracker file")
	samplingTimeHeader := flag.String("sampling-time-header", "", "The name of the header that corresponds to the sampling time")
	rateInterval := flag.Duration("rate-interval", 30*time.Second, "The window in which we calculate the rate.")
	outputFilePath := flag.String("output", "", "The path to the output file.")
	flag.Parse()

	prometheusEndpoint := connection.CreateEndpoint(*prometheusHost)
	if prometheusEndpoint == nil {
		panic("invalid prometheus endpoint")
	}
	file, err := ioutil.ReadFile(*trackerFilePath)
	if err != nil {
		panic(err)
	}
	trackerContent := strings.Split(string(file), "\n")

	samplingTimes := extractSamplingTimes(trackerContent, *samplingTimeHeader)
	if len(samplingTimes) == 0 {
		panic("no timestamps passed")
	}
	fmt.Printf("Received sampling points for %d experiments from %v to %v", len(samplingTimes), samplingTimes[0], samplingTimes[len(samplingTimes)-1])

	reader := experiment.NewResultReader(connection.CreateEndpoint(*clientHost), connection.CreateEndpoint(*prometheusHost), *rateInterval)

	resultContent := make([]string, len(samplingTimes)+1)
	resultContent[0] = strings.Join(reader.ReadHeaders(), resultJoiner)
	for i, samplingTime := range samplingTimes {
		resultContent[i+1] = convertToString(reader.ReadExperimentResults(samplingTime))
	}

	utils.Must(utils.WriteFile(*outputFilePath, []byte(strings.Join(merge(trackerContent, resultContent), "\n"))))
}

func merge(content1, content2 []string) []string {
	if len(content1) != len(content2) {
		panic("contents not of equal length")
	}
	result := make([]string, len(content1))
	for i := 0; i < len(result); i++ {
		result[i] = content1[i] + resultJoiner + content2[i]
	}
	return result
}

func extractSamplingTimes(trackerContent []string, samplingTimeHeader string) []time.Time {
	headers := trackerContent[0]
	data := trackerContent[1:]
	samplingTimeIndex := findIndex(strings.Split(headers, resultJoiner), samplingTimeHeader)
	if samplingTimeIndex < 0 {
		panic("wrong sampling-time header name: " + samplingTimeHeader + ". not found in " + headers)
	}
	samplingTimes := make([]time.Time, len(data))
	for i, line := range data {
		if line == "" {
			continue //TODO: Remove this
		}
		unixTimestamp, err := strconv.ParseInt(strings.Split(line, resultJoiner)[samplingTimeIndex], 10, 64)
		if err != nil {
			panic("invalid unix timestamp given in line " + strconv.Itoa(i) + ": " + line + ". " + err.Error())
		}
		samplingTimes[i] = time.Unix(unixTimestamp, 0)
		if samplingTimes[i].After(time.Now()) {
			panic("future sampling not allowed")
		}
	}
	return samplingTimes
}

func findIndex(haystack []string, needle string) int {
	for i, item := range haystack {
		if item == needle {
			return i
		}
	}
	return -1
}

func convertToString(results []float64) string {
	return strings.Trim(strings.Join(strings.Fields(fmt.Sprint(results)), resultJoiner), "[]")
}
