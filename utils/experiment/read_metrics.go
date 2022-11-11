package experiment

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

type valueExtractor = func(*data) float64
type metric struct {
	query, description, unit string
	extractor                valueExtractor
}
type ResultReader struct {
	server  *connection.Endpoint
	metrics []*metric
}

func NewResultReader(server *connection.Endpoint, rateInterval time.Duration) *ResultReader {
	return &ResultReader{
		server: server,
		metrics: []*metric{
			{
				query:       "sc_e2e_requests",
				description: "Total requests",
				unit:        "number",
				extractor:   floatExtractor,
			},
			{
				query:       fmt.Sprintf("rate(sc_e2e_responses{status=\"VALID\"}[%v])", rateInterval),
				description: "Throughput (Valid)",
				unit:        "TPS",
				extractor:   floatExtractor,
			},
			{
				query:       fmt.Sprintf("rate(sc_e2e_responses{status=\"INVALID_SIGNATURE\"}[%v])", rateInterval),
				description: "Throughput (Invalid sig)",
				unit:        "TPS",
				extractor:   floatExtractor,
			},
			{
				query:       fmt.Sprintf("rate(sc_e2e_responses{status=\"DOUBLE_SPEND\"}[%v])", rateInterval),
				description: "Throughput (Double spend)",
				unit:        "TPS",
				extractor:   floatExtractor,
			},
			{
				query:       fmt.Sprintf("histogram_quantile(%.2f,sc_generator_latency_bucket{status=\"VALID\"})", 0.99),
				description: "99%-Latency (Valid)",
				unit:        "ns",
				extractor:   floatExtractor,
			},
			{
				query:       fmt.Sprintf("histogram_quantile(%.2f,sc_generator_latency_bucket{status=\"INVALID_SIGNATURE\"})", 0.99),
				description: "99%-Latency (Invalid sig)",
				unit:        "ns",
				extractor:   floatExtractor,
			},
			{
				query:       fmt.Sprintf("histogram_quantile(%.2f,sc_generator_latency_bucket{status=\"DOUBLE_SPEND\"})", 0.99),
				description: "99%-Latency (Double spend)",
				unit:        "ns",
				extractor:   floatExtractor,
			},
		},
	}
}

var floatExtractor = func(d *data) float64 {
	if len(d.Result) == 0 {
		return 0
	}
	r, err := strconv.ParseFloat(d.Result[0].Value[1].(string), 64)
	if err != nil {
		return 0
	}
	return r
}

func (r *ResultReader) ReadHeaders() []string {
	headers := make([]string, len(r.metrics))
	for i, metric := range r.metrics {
		headers[i] = metric.description
	}
	return headers
}

func (r *ResultReader) ReadExperimentResults(timestamp time.Time) []float64 {
	results := make([]float64, len(r.metrics))
	for i, metric := range r.metrics {
		resp, err := r.fetchMetric(metric.query, timestamp)
		fmt.Printf("Received: %v, %v", resp, err)
		if err == nil {
			results[i] = metric.extractor(resp)
		}
	}
	return results
}

type output struct {
	Status string
	Data   data
}
type data struct {
	ResultType string
	Result     []result
}
type result struct {
	Metric map[string]interface{}
	Value  []interface{}
}

func (r *ResultReader) fetchMetric(query string, timestamp time.Time) (*data, error) {
	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/query?query=%s&time=%d", r.server.Address(), query, timestamp.Unix()))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, errors.New(fmt.Sprintf("request for metric %s failed", query))
	}
	out, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var response output
	err = json.Unmarshal(out, &response)
	if err != nil {
		return nil, err
	}
	if response.Status != "success" {
		return nil, errors.New(fmt.Sprintf("response parsing for metric %s failed", query))
	}
	return &response.Data, nil
}
