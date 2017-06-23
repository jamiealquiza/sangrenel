package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

var (
	graphiteIp    string
	graphitePort  string
	metricsPrefix string

	metrics         = make(map[string]float64)
	metricsOutgoing = make(chan map[string]float64, 30)
)

func init() {
	hostname, _ := os.Hostname()
	flag.StringVar(&graphiteIp, "graphite-ip", "", "Destination Graphite IP address")
	flag.StringVar(&graphitePort, "graphite-port", "", "Destination Graphite plaintext port")
	flag.StringVar(&metricsPrefix, "graphite-metrics-prefix", hostname, "Top-level Graphite namespace prefix (defaults to hostname)")
}

func graphiteWriter() {
	for {
		// Connect to Graphite.
		graphite, err := net.Dial("tcp", graphiteIp+":"+graphitePort)
		if err != nil {
			log.Printf("Graphite unreachable: %s", err)
			time.Sleep(30 * time.Second)
			continue
		}

		// Fetch / ship metrics.
		metrics := <-metricsOutgoing
		ts := int(metrics["timestamp"])
		delete(metrics, "timestamp")

		for k, v := range metrics {
			_, err := fmt.Fprintf(graphite, "%s.sangrenel.%s %f %d\n", metricsPrefix, k, v, ts)
			if err != nil {
				log.Printf("Error flushing to Graphite: %s", err)
			}
		}

		log.Println("Metrics flushed to Graphite")
		graphite.Close()
	}
}
