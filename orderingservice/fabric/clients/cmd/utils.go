package cmd

import (
	"bufio"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

type LabelSet = map[string]string

type PrometheusMetricClient struct {
	client    *http.Client
	channelId string
}

func NewPrometheusMetricClient(channelId string, rootCAPaths ...string) *PrometheusMetricClient {
	return &PrometheusMetricClient{client: connection.SecureClient(rootCAPaths...), channelId: channelId}
}

func (c *PrometheusMetricClient) GetLeader(endpoints []*connection.Endpoint, retry time.Duration) (int, *connection.Endpoint, error) {
	for {
		for i, endpoint := range endpoints {
			isLeader, err := c.getPrometheusMetric(endpoint, "consensus_etcdraft_is_leader", LabelSet{"channel": c.channelId})
			if err != nil {
				return -1, nil, err
			}
			if isLeader == 1 {
				return i, endpoint, nil
			}
		}
		fmt.Printf("No leader elected yet. Retrying in %v.\n", retry)
		<-time.After(retry)
	}
}

func (c *PrometheusMetricClient) getPrometheusMetric(endpoint *connection.Endpoint, metric string, labels LabelSet) (float64, error) {
	response, err := c.client.Get(fmt.Sprintf("http://%s/metrics", endpoint.Address()))
	defer response.Body.Close()
	if err != nil {
		return 0, err
	}
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return 0, err
		}
		lineMetric, lineLabels, lineValue, err := parseLine(line)
		if err != nil {
			return 0, err
		}
		if lineMetric != metric {
			continue
		}
		for labelKey, labelValue := range labels {
			if lineLabelValue, ok := lineLabels[labelKey]; !ok || labelValue != lineLabelValue {
				continue
			}
		}
		return lineValue, nil
	}
}

func parseLine(line string) (string, LabelSet, float64, error) {
	if isComment(line) {
		return "", nil, 0, nil
	}
	tokens := strings.Split(strings.TrimSpace(line), " ")
	value, err := strconv.ParseFloat(tokens[len(tokens)-1], 64)
	if err != nil {
		return "", nil, 0, err
	}
	labeledMetric := strings.Join(tokens[:len(tokens)-1], " ")

	tokens = strings.Split(labeledMetric, "{")
	metricName := tokens[0]
	labelSet := make(LabelSet)
	if len(tokens) > 1 {
		labels := strings.Join(tokens[1:], "{")
		keyValues := strings.TrimSuffix(strings.TrimPrefix(labels, "{"), "}")
		for _, keyValue := range strings.Split(keyValues, ",") {
			values := strings.Split(keyValue, "=")
			labelSet[values[0]] = values[1]
		}
	}
	return metricName, labelSet, value, nil

}

func isComment(line string) bool {
	return strings.HasPrefix(line, "#")
}
