package handler

import (
	"fmt"
	"fullerite/metric"
	"net"
	"sort"
	"time"

	"github.com/Sirupsen/logrus"
)

// Graphite type
type Graphite struct {
	BaseHandler
	server string
	port   string
}

// NewGraphite returns a new Graphite handler.
func NewGraphite() *Graphite {
	g := new(Graphite)
	g.name = "Graphite"
	g.maxBufferSize = DefaultBufferSize
	g.timeout = time.Duration(DefaultHandlerTimeoutSec * time.Second)
	g.log = logrus.WithFields(logrus.Fields{"app": "fullerite", "pkg": "handler", "handler": "Graphite"})
	g.channel = make(chan metric.Metric)
	return g
}

// Configure accepts the different configuration options for the Graphite handler
func (g *Graphite) Configure(config map[string]interface{}) {
	if server, exists := config["server"]; exists == true {
		g.server = server.(string)
	} else {
		g.log.Error("There was no server specified for the Graphite Handler, there won't be any emissions")
	}
	if port, exists := config["port"]; exists == true {
		g.port = port.(string)
	} else {
		g.log.Error("There was no port specified for the Graphite Handler, there won't be any emissions")
	}
	if timeout, exists := config["timeout"]; exists == true {
		g.timeout = time.Duration(timeout.(float64)) * time.Second
	}
	if bufferSize, exists := config["max_buffer_size"]; exists == true {
		g.maxBufferSize = int(bufferSize.(float64))
	}
}

// Run sends metrics in the channel to the graphite server.
func (g *Graphite) Run() {
	datapoints := make([]string, 0, g.maxBufferSize)

	lastEmission := time.Now()
	for incomingMetric := range g.Channel() {
		datapoint := g.convertToGraphite(incomingMetric)
		g.log.Debug("Graphite datapoint: ", datapoint)
		datapoints = append(datapoints, datapoint)
		if time.Since(lastEmission).Seconds() >= float64(g.interval) || len(datapoints) >= g.maxBufferSize {
			g.emitMetrics(datapoints)
			lastEmission = time.Now()
			datapoints = make([]string, 0, g.maxBufferSize)
		}
	}
}

func (g *Graphite) convertToGraphite(incomingMetric metric.Metric) (datapoint string) {
	//orders dimensions so datapoint keeps consistent name
	var keys []string
	dimensions := incomingMetric.GetDimensions(g.DefaultDimensions())
	for k := range dimensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	datapoint = g.Prefix() + incomingMetric.Name
	for _, key := range keys {
		datapoint = fmt.Sprintf("%s.%s.%s", datapoint, key, dimensions[key])
	}
	datapoint = fmt.Sprintf("%s %f %d\n", datapoint, incomingMetric.Value, time.Now().Unix())
	return datapoint
}

func (g *Graphite) emitMetrics(datapoints []string) {
	g.log.Info("Starting to emit ", len(datapoints), " datapoints")

	if len(datapoints) == 0 {
		g.log.Warn("Skipping send because of an empty payload")
		return
	}

	addr := fmt.Sprintf("%s:%s", g.server, g.port)
	conn, err := net.DialTimeout("tcp", addr, g.timeout)
	if err != nil {
		g.log.Error("Failed to connect ", addr)
	} else {
		for _, datapoint := range datapoints {
			fmt.Fprintf(conn, datapoint)
		}
	}
}
