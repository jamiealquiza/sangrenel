// The MIT License (MIT)
//
// Copyright (c) 2015 Jamie Alquiza
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.
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
