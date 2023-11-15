package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/briandowns/spinner"
	"github.com/mitchellh/colorstring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"gopkg.in/yaml.v2"
)

var serverEndpoint = "localhost:8080"

type workload struct {
	Load     load   `mapstructure:"load"`
	Endpoint string `mapstructure:"endpoint"`
	Loop     bool   `mapstructure:"loop"`
}

type load struct {
	Interval  time.Duration `mapstructure:"interval"`
	TargetTps []float64     `mapstructure:"tps"`
}

func loadWorkloadFromYaml(yamlPath string) *workload {
	pp := &workload{}

	yamlPath, err := filepath.Abs(yamlPath)
	utils.Must(err)
	fmt.Printf("Loading workload from %s\n", yamlPath)

	yamlFile, err := os.ReadFile(yamlPath)
	utils.Must(err)

	err = yaml.Unmarshal(yamlFile, pp)
	utils.Must(err)

	return pp
}

func setLimit(limit int) {
	jsonBody := []byte(fmt.Sprintf(`{"limit": %d}`, limit))
	bodyReader := bytes.NewReader(jsonBody)
	requestURL := fmt.Sprintf("http://%s/setLimits", serverEndpoint)
	callPost(requestURL, bodyReader)
}

func callPost(url string, body io.Reader) {
	req, err := http.NewRequest(http.MethodPost, url, body)
	utils.Must(err)

	res, err := http.DefaultClient.Do(req)
	utils.Must(err)

	if res.StatusCode != http.StatusOK {
		utils.Must(fmt.Errorf("received status %v, but expected %v", res.Status, http.StatusOK))
	}
}

func main() {
	// read path for workload file
	yamlPathPtr := flag.String("path", "testdata/remote-limiter/eu_sat_peak.yaml", "path to the remote workload file")
	flag.Parse()

	p := loadWorkloadFromYaml(*yamlPathPtr)
	fmt.Printf("Endpoint: %v\n", p.Endpoint)

	serverEndpoint = p.Endpoint
	wait := p.Load.Interval
	numLoads := len(p.Load.TargetTps)

	// something bit more visual
	s := spinner.New(spinner.CharSets[26], 200*time.Millisecond)
	s.Color("green")

	for {
		fmt.Println(colorstring.Color("[green]-> starting"))
		for i, t := range p.Load.TargetTps {
			// call remote
			targetTPS := int(math.Round(t))
			setLimit(targetTPS)

			// update ui and sleep
			s.Prefix = colorstring.Color(fmt.Sprintf("[green][[reset]%d/%d[green]][reset] set limit to %v TPS and wait %v ", i+1, numLoads, targetTPS, wait))
			s.Restart()
			time.Sleep(wait)
			s.Stop()
		}

		if !p.Loop {
			break
		}
	}

	// unset limter
	setLimit(0)

	fmt.Println(colorstring.Color("[green]-> done! [reset]thanks and goodbye"))
}
