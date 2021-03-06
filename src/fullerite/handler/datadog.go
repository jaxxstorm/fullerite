package handler

import (
	"fullerite/metric"

	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"time"

	"github.com/Sirupsen/logrus"
)

// Datadog handler
type Datadog struct {
	BaseHandler
	endpoint string
	apiKey   string
}

type datadogPayload struct {
	Series []datadogMetric `json:"series"`
}

type datadogMetric struct {
	Metric     string         `json:"metric"`
	Points     []datadogPoint `json:"points"`
	MetricType string         `json:"type"`
	Host       string         `json:"host"`
	Tags       []string       `json:"tags"`
}

type datadogPoint [2]float64

// NewDatadog returns a new Datadog handler
func NewDatadog() *Datadog {
	d := new(Datadog)
	d.name = "Datadog"
	d.maxBufferSize = DefaultBufferSize
	d.timeout = time.Duration(DefaultHandlerTimeoutSec * time.Second)
	d.log = logrus.WithFields(logrus.Fields{"app": "fullerite", "pkg": "handler", "handler": "Datadog"})
	d.channel = make(chan metric.Metric)
	return d
}

// Configure the Datadog handler
func (d *Datadog) Configure(config map[string]interface{}) {
	if apiKey, exists := config["apiKey"]; exists == true {
		d.apiKey = apiKey.(string)
	} else {
		d.log.Error("There was no API key specified for the Datadog handler, there won't be any emissions")
	}
	if endpoint, exists := config["endpoint"]; exists == true {
		d.endpoint = endpoint.(string)
	} else {
		d.log.Error("There was no endpoint specified for the Datadog Handler, there won't be any emissions")
	}
	if timeout, exists := config["timeout"]; exists == true {
		d.timeout = time.Duration(timeout.(float64)) * time.Second
	}
	if bufferSize, exists := config["max_buffer_size"]; exists == true {
		d.maxBufferSize = int(bufferSize.(float64))
	}
}

// Run runs the Datadog handler
func (d *Datadog) Run() {
	datapoints := make([]datadogMetric, 0, d.maxBufferSize)

	lastEmission := time.Now()
	for incomingMetric := range d.Channel() {
		datapoint := d.convertToDatadog(incomingMetric)
		d.log.Debug("Datadog datapoint: ", datapoint)
		datapoints = append(datapoints, datapoint)
		if time.Since(lastEmission).Seconds() >= float64(d.interval) || len(datapoints) >= d.maxBufferSize {
			d.emitMetrics(datapoints)
			lastEmission = time.Now()
			datapoints = make([]datadogMetric, 0, d.maxBufferSize)
		}
	}
}

func (d *Datadog) convertToDatadog(incomingMetric metric.Metric) (datapoint datadogMetric) {
	dog := new(datadogMetric)
	dog.Metric = incomingMetric.Name
	dog.Points = makeDatadogPoints(incomingMetric)
	dog.MetricType = incomingMetric.MetricType
	if host, ok := incomingMetric.GetDimensionValue("host", d.DefaultDimensions()); ok {
		dog.Host = host
	} else {
		dog.Host = "unknown"
	}
	dog.Tags = d.serializedDimensions(incomingMetric)
	return *dog
}

func (d *Datadog) emitMetrics(series []datadogMetric) {
	d.log.Info("Starting to emit ", len(series), " datapoints")

	if len(series) == 0 {
		d.log.Warn("Skipping send because of an empty payload")
		return
	}

	p := datadogPayload{Series: series}
	payload, err := json.Marshal(p)
	if err != nil {
		d.log.Error("Failed marshaling datapoints to Datadog format")
		d.log.Error("Dropping Datadog datapoints ", series)
		return
	}

	apiURL := fmt.Sprintf("%s/series?api_key=%s", d.endpoint, d.apiKey)
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(payload))
	if err != nil {
		d.log.Error("Failed to create a request to endpoint ", d.endpoint)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	transport := http.Transport{
		Dial: d.dialTimeout,
	}
	client := &http.Client{
		Transport: &transport,
	}
	rsp, err := client.Do(req)
	if err != nil {
		d.log.Error("Failed to complete POST ", err)
		return
	}

	defer rsp.Body.Close()
	if (rsp.StatusCode == http.StatusOK) || (rsp.StatusCode == http.StatusAccepted) {
		d.log.Info("Successfully sent ", len(series), " datapoints to Datadog")
	} else {
		body, _ := ioutil.ReadAll(rsp.Body)
		d.log.Error("Failed to post to Datadog @", d.endpoint,
			" status was ", rsp.Status,
			" rsp body was ", string(body),
			" payload was ", string(payload))
		return
	}

}

func (d *Datadog) dialTimeout(network, addr string) (net.Conn, error) {
	return net.DialTimeout(network, addr, d.timeout)
}

func (d *Datadog) serializedDimensions(m metric.Metric) (dimensions []string) {
	for name, value := range m.GetDimensions(d.DefaultDimensions()) {
		dimensions = append(dimensions, name+":"+value)
	}
	return dimensions
}

func makeDatadogPoints(m metric.Metric) []datadogPoint {
	point := datadogPoint{float64(time.Now().Unix()), m.Value}
	return []datadogPoint{point}
}
