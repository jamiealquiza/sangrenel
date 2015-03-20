// The MIT License (MIT)
//
// Copyright (c) 2014, 2015 Jamie Alquiza
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
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	kafka "github.com/jamiealquiza/sangrenel/vendor/github.com/Shopify/sarama"
)

var (
	// Configs.
	brokers        []string
	topic          string
	msgSize        int64
	msgRate        int64
	batchSize      int
	compressionOpt string
	compression    kafka.CompressionCodec
	clients        int
	producers      int
	noop           bool

	// Character selection from which random messages are generated.
	chars = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890!@#$^&*(){}][:<>.")

	// Counters / misc.
	signals        = make(chan os.Signal)
	killClients = make(chan bool, 24)
	sentCntr        = make(chan int64, 1)
	latency         []float64
	latencies    = make(chan float64, 1)
	resetLatencies   = make(chan bool, 1)
)

func init() {
	flag.StringVar(&topic, "topic", "sangrenel", "Topic to publish to")
	flag.Int64Var(&msgSize, "size", 300, "Message size in bytes")
	flag.Int64Var(&msgRate, "rate", 100000000, "Apply a global message rate limit")
	flag.IntVar(&batchSize, "batch", 0, "Max messages per batch. Defaults to unlimited (0).")
	flag.StringVar(&compressionOpt, "compression", "none", "Message compression: none, gzip, snappy")
	flag.BoolVar(&noop, "noop", false, "Test message generation performance, do not transmit messages")
	flag.IntVar(&clients, "clients", 1, "Number of Kafka client workers")
	flag.IntVar(&producers, "producers", 5, "Number of producer instances per client")
	brokerString := flag.String("brokers", "localhost:9092", "Comma delimited list of Kafka brokers")
	flag.Parse()

	brokers = strings.Split(*brokerString, ",")

	switch compressionOpt {
	case "gzip":
		compression = kafka.CompressionGZIP
	case "snappy":
		compression = kafka.CompressionSnappy
	case "none":
		compression = kafka.CompressionNone
	default:
		fmt.Printf("Invalid compression option: %s\n", compressionOpt)
		os.Exit(1)
	}

	sentCntr <- 0
	runtime.GOMAXPROCS(runtime.NumCPU())
}

// clientProducer generates random messages and writes to Kafka.
// Workers track and limit message rates using incrSent() and fetchSent().
// Default 5 instances of clientProducer are created under each Kafka client.
func clientProducer(c kafka.Client) {
	producer, err := kafka.NewSyncProducerFromClient(c)
	if err != nil {
		log.Println(err.Error())
	}
	defer producer.Close()

	// Instantiate rand per producer to avoid mutex contention.
	source := rand.NewSource(time.Now().UnixNano())
	generator := rand.New(source)
	msgData := make([]byte, msgSize)

	// Use a local accumulator then periodically update global counter.
	// Global counter can become a bottleneck with too many threads.
	tick := time.Tick(3 * time.Millisecond)
	var n int64

	for {
		// Message rate limit works by having all clientProducer loops incrementing
		// a global counter and tracking the aggregate per-second progress.
		// If the configured rate is met, the worker will sleep
		// for the remainder of the 1 second window.
		rateEnd := time.Now().Add(time.Second)
		countStart := fetchSent()
		var start time.Time
		for fetchSent()-countStart < msgRate {
			randMsg(msgData, *generator)
			msg := &kafka.ProducerMessage{Topic: topic, Value: kafka.ByteEncoder(msgData)}
			// We start timing after the message is created.
			// This ensures latency metering from the time between message sent and receiving an ack.
			start = time.Now()
			_, _, err = producer.SendMessage(msg)
			if err != nil {
				log.Println(err)
			} else {
				// Increment global sent count and fire off time since start value into the latency channel.
				n++
				select {
				case <-tick:
					incrSent(n)
					n = 0
				default:
					break
				}
				latencies <- time.Since(start).Seconds() * 1000
			}
		}
		// If the global per-second rate limit was met,
		// the inner loop breaks and the outer loop sleeps for the second remainder.
		time.Sleep(rateEnd.Sub(time.Now()) + time.Since(start))
	}
}

// clientDummyProducer is a dummy function that kafkaClient calls if noop is True.
// It is used in place of starting actual Kafka client connections to test message creation performance.
func clientDummyProducer() {
	source := rand.NewSource(time.Now().UnixNano())
	generator := rand.New(source)
	msg := make([]byte, msgSize)

	tick := time.Tick(10 * time.Millisecond)
	var n int64

	for {
		randMsg(msg, *generator)
		n++
		select {
		case <-tick:
			incrSent(n)
			n = 0
		default:
			break
		}
	}
}

// kafkaClient initializes a connection to a Kafka cluster and
// initializes one or more clientProducer() (producer instances).
func kafkaClient(n int) {
	switch noop {
	// If not noop, actually fire up Kafka connections and send messages.
	case false:
		cId := "client_" + strconv.Itoa(n)

		conf := kafka.NewConfig()
		if compression != kafka.CompressionNone {
			conf.Producer.Compression = compression
		}
		conf.Producer.Flush.MaxMessages = batchSize

		client, err := kafka.NewClient(brokers, conf)
		if err != nil {
			log.Println(err)
			os.Exit(1)
		} else {
			log.Printf("%s connected\n", cId)
		}
		for i := 0; i < producers; i++ {
			go clientProducer(client)
		}
	// If noop, we're not creating connections at all.
	// Just generate messages and burn CPU.
	default:
		for i := 0; i < producers; i++ {
			go clientDummyProducer()
		}
	}
	<-killClients
}

// Returns a random message generated from the chars byte slice.
// Message length of m bytes as defined by msgSize.
func randMsg(m []byte, generator rand.Rand) {
	for i := range m {
		m[i] = chars[generator.Intn(len(chars))]
	}
}

// Thread-safe global counter functions.
func incrSent(n int64) {
	i := <-sentCntr
	sentCntr <- i + n
}
func fetchSent() int64 {
	i := <-sentCntr
	sentCntr <- i
	return i
}

// Thread-safe receiver for latency values captured by all producer goroutines.
// May want to do something smart about this to limit time to sort
// huge slices in high-throughput configurations where lots of latency values are received.
func latencyAggregator() {
	for {
		select {
		case i := <-latencies:
			latency = append(latency, i)
		case <-resetLatencies:
			latency = latency[:0]
		}
	}
}

// Calculates aggregate raw message output in networking friendly units.
// Gives an idea of minimum network traffic being generated.
func calcOutput(n int64) (float64, string) {
	m := (float64(n) / 5) * float64(msgSize)
	var o string
	switch {
	case m >= 131072:
		o = strconv.FormatFloat(m/131072, 'f', 0, 64) + "Mb/sec"
	case m < 131072:
		o = strconv.FormatFloat(m/1024, 'f', 0, 64) + "KB/sec"
	}
	return m, o
}

// Fetches & resets current latencies set held by 'latencyAggregator()'.
// Sorts then averages the 90th percentile worst latencies.
func calcLatency() float64 {
	var avg float64
	// With 'noop', we don't have latencies to operate on.
	switch noop {
	case true:
		break
	default:
		// Fetch values.
		lat := latency
		// Issue the current values to be cleared.
		resetLatencies <- true
		// Sort and sum values.
		sort.Float64s(lat)
		var sum float64
		// Get percentile count and values, sum values.
		topn := int(float64(len(lat)) * 0.90)
		for i := topn; i < len(lat); i++ {
			sum += lat[i]
		}
		// Calc average.
		avg = sum / float64(len(lat)-topn)
	}
	return avg
}

func main() {
	// Listens for signals.
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	// Fire up misc. tasks.
	go latencyAggregator()
	if graphiteIp != "" {
		go graphiteWriter()
	}

	// Print Sangrenel startup info.
	fmt.Println("\n::: Sangrenel :::")
	fmt.Printf("\nStarting %d client workers, %d producers per worker\n", clients, producers)
	fmt.Printf("Message size %d bytes, %d message limit per batch\n", msgSize, batchSize)
	switch compressionOpt {
	case "none":
		fmt.Println("Compression: none\n")
	case "gzip":
		fmt.Println("Compression: GZIP\n")
	case "snappy":
		fmt.Println("Compression: Snappy\n")
	}

	// Start client workers.
	for i := 0; i < clients; i++ {
		go kafkaClient(i + 1)
	}

	// Start Sangrenel periodic info output.
	tick := time.Tick(5 * time.Second)
	// Count mile-markers for tracking message rates.
	var currCnt, lastCnt int64
	for {
		select {
		case <-tick:
			// Set last and current to last read sent count.
			lastCnt = currCnt

			// Get actual current sent count, then delta from last count.
			// Delta is divided by update interval (5s) for per-second rate over a window.
			currCnt = fetchSent()
			deltaCnt := currCnt - lastCnt

			outputBytes, outputString := calcOutput(deltaCnt)

			// Update the metrics map which is also passed to the Graphite writer.
			metrics["rate"] = float64(deltaCnt/5)
			metrics["90th"] = calcLatency() // Well, this technically appends a small latency to the 5s interval.
			metrics["output"] = outputBytes
			now := time.Now()
			ts := float64(now.Unix())
			metrics["timestamp"] = ts
			if graphiteIp != "" {
				metricsOutgoing <- metrics
			}

			log.Printf("Generating %s @ %.0f messages/sec | topic: %s | %.2fms 90%%ile latency\n",
				outputString,
				metrics["rate"],
				topic,
				metrics["90th"])

		// Waits for signals. Currently just brutally kills Sangrenel.
		case <-signals:
			fmt.Println("Killing Connections")
			for i := 0; i < clients; i++ {
				killClients <- true
			}
			close(killClients)
			time.Sleep(2 * time.Second)
			os.Exit(0)
		}
	}
}
