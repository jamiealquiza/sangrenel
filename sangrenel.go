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
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jamiealquiza/tachymeter"
	"gopkg.in/Shopify/sarama.v1"
)

type config struct {
	brokers            []string
	topic              string
	msgSize            int
	msgRate            uint64
	batchSize          int
	compression        sarama.CompressionCodec
	workers            int
	producersPerWorker int
	noop               bool
}

var (
	Config = &config{}

	// Character selection for random messages.
	chars = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890!@#$^&*(){}][:<>.")

	// Counters / misc.
	sentCnt uint64
)

func init() {
	flag.StringVar(&Config.topic, "topic", "sangrenel", "Kafka topic to produce to")
	flag.IntVar(&Config.msgSize, "message-size", 300, "Message size (bytes)")
	flag.Uint64Var(&Config.msgRate, "produce-rate", 100000000, "Global producer rate limit")
	flag.IntVar(&Config.batchSize, "message-batch-size", 1, "Messages per batch")
	compression := flag.String("compression", "none", "Message compression: none, gzip, snappy")
	flag.BoolVar(&Config.noop, "noop", false, "Test message generation performance (does not connect to Kafka)")
	flag.IntVar(&Config.workers, "workers", 1, "Number of Kafka client workers")
	flag.IntVar(&Config.producersPerWorker, "producers-per-worker", 5, "Number of producer goroutines per client worker")
	brokerString := flag.String("brokers", "localhost:9092", "Comma delimited list of Kafka brokers")
	flag.Parse()

	Config.brokers = strings.Split(*brokerString, ",")

	switch *compression {
	case "gzip":
		Config.compression = sarama.CompressionGZIP
	case "snappy":
		Config.compression = sarama.CompressionSnappy
	case "none":
		Config.compression = sarama.CompressionNone
	default:
		fmt.Printf("Invalid compression option: %s\n", *compression)
		os.Exit(1)
	}
}

func main() {
	if graphiteIp != "" {
		go graphiteWriter()
	}

	// Print Sangrenel startup info.
	fmt.Println("\n::: Sangrenel :::")
	fmt.Printf("\nStarting %d client workers, %d producers per worker\n", Config.workers, Config.producersPerWorker)
	fmt.Printf("Message size %d bytes, %d message limit per batch\n", Config.msgSize, Config.batchSize)

	switch Config.compression {
	case sarama.CompressionNone:
		fmt.Println("Compression: none")
	case sarama.CompressionGZIP:
		fmt.Println("Compression: GZIP")
	case sarama.CompressionSnappy:
		fmt.Println("Compression: Snappy")
	}

	t := tachymeter.New(&tachymeter.Config{Size: 300000, Safe: true})

	// Start client workers.
	for i := 0; i < Config.workers; i++ {
		go kafkaClient(i+1, t)
	}

	// Start Sangrenel periodic info output.
	tick := time.Tick(5 * time.Second)

	var currCnt, lastCnt uint64
	start := time.Now()
	for {
		<-tick
		// Set tachymeter wall time.
		t.SetWallTime(time.Since(start))

		// Set last and current to last read sent count.
		lastCnt = currCnt

		// Get actual current sent count, then delta from last count.
		// Delta is divided by update interval (5s) for per-second rate over a window.
		currCnt = atomic.LoadUint64(&sentCnt)
		deltaCnt := currCnt - lastCnt

		stats := t.Calc()

		outputBytes, outputString := calcOutput(deltaCnt)

		// Update the metrics map for the Graphite writer.
		metrics["rate"] = stats.Rate.Second
		metrics["output"] = outputBytes
		metrics["p99"] = (float64(stats.Time.P99.Nanoseconds()) / 1000) / 1000
		// Add ts for Graphite.
		now := time.Now()
		ts := float64(now.Unix())
		metrics["timestamp"] = ts

		if graphiteIp != "" {
			metricsOutgoing <- metrics
		}

		fmt.Println()
		log.Printf("Generating %s @ %.0f messages/sec | topic: %s | %.2fms p99 latency\n",
			outputString,
			metrics["rate"],
			Config.topic,
			metrics["p99"])

		stats.Dump()

		// Check if the tacymeter size needs to be increased
		// to avoid sampling. Otherwise, just reset it.
		if int(deltaCnt) > len(t.Times) {
			newTachy := tachymeter.New(&tachymeter.Config{Size: int(2 * deltaCnt), Safe: true})
			// This is actually dangerous;
			// this could swap in a tachy with unlocked
			// mutexes while the current one has locks held.
			*t = *newTachy
		} else {
			t.Reset()
		}

		// Reset interval time.
		start = time.Now()
	}
}

// clientProducer generates random messages and writes to Kafka.
// Workers track and limit message rates using incrSent() and fetchSent().
// Default 5 instances of clientProducer are created under each Kafka client.
func clientProducer(c sarama.Client, t *tachymeter.Tachymeter) {
	producer, err := sarama.NewSyncProducerFromClient(c)
	if err != nil {
		log.Println(err.Error())
	}
	defer producer.Close()

	source := rand.NewSource(time.Now().UnixNano())
	generator := rand.New(source)
	msgData := make([]byte, Config.msgSize)

	// Use a local accumulator then periodically update global counter.
	// Global counter can become a bottleneck with too many threads.
	// tick := time.Tick(2 * time.Millisecond)
	var n int64
	var times [10]time.Duration

	for {
		// Message rate limit works by having all clientProducer loops incrementing
		// a global counter and tracking the aggregate per-second progress.
		// If the configured rate is met, the worker will sleep
		// for the remainder of the 1 second window.
		rateEnd := time.Now().Add(time.Second)
		countStart := atomic.LoadUint64(&sentCnt)
		var start time.Time
		for atomic.LoadUint64(&sentCnt)-countStart < Config.msgRate {
			randMsg(msgData, *generator)
			msg := &sarama.ProducerMessage{Topic: Config.topic, Value: sarama.ByteEncoder(msgData)}

			start = time.Now()
			_, _, err = producer.SendMessage(msg)
			if err != nil {
				log.Println(err)
			} else {
				// Increment global counter and
				// tachymeter every 10 messages.
				n++
				times[n-1] = time.Since(start)
				if n == 10 {
					atomic.AddUint64(&sentCnt, 10)
					for _, ts := range times {
						t.AddTime(ts)
					}
					n = 0
				}
			}
		}
		// If the global per-second rate limit was met,
		// the inner loop breaks and the outer loop sleeps for the second remainder.
		time.Sleep(rateEnd.Sub(time.Now()) + time.Since(start))
	}
}

// clientDummyProducer is a dummy function that kafkaClient calls if Config.noop is True.
// It is used in place of starting actual Kafka client connections to test message creation performance.
func clientDummyProducer(t *tachymeter.Tachymeter) {
	source := rand.NewSource(time.Now().UnixNano())
	generator := rand.New(source)
	msg := make([]byte, Config.msgSize)

	var n int64
	var times [10]time.Duration

	for {
		start := time.Now()
		randMsg(msg, *generator)

		// Increment global counter and
		// tachymeter every 10 messages.
		n++
		times[n-1] = time.Since(start)
		if n == 10 {
			atomic.AddUint64(&sentCnt, 10)
			for _, ts := range times {
				t.AddTime(ts)
			}
			n = 0
		}
	}
}

// kafkaClient initializes a connection to a Kafka cluster and
// initializes one or more clientProducer() (producer instances).
func kafkaClient(n int, t *tachymeter.Tachymeter) {
	switch Config.noop {
	// If not noop, actually fire up Kafka connections and send messages.
	case false:
		cId := "client_" + strconv.Itoa(n)

		conf := sarama.NewConfig()
		conf.Producer.Compression = Config.compression
		conf.Producer.Return.Successes = true
		conf.Producer.Flush.MaxMessages = Config.batchSize
		conf.Producer.MaxMessageBytes = Config.msgSize + 50

		client, err := sarama.NewClient(Config.brokers, conf)
		if err != nil {
			log.Println(err)
			os.Exit(1)
		} else {
			log.Printf("%s connected\n", cId)
		}

		for i := 0; i < Config.producersPerWorker; i++ {
			go clientProducer(client, t)
		}
	// If noop, we're not creating connections at all.
	// Just generate messages and burn CPU.
	default:
		for i := 0; i < Config.producersPerWorker; i++ {
			go clientDummyProducer(t)
		}
	}

	wait := make(chan bool)
	<-wait
}

// Returns a random message generated from the chars byte slice.
// Message length of m bytes as defined by Config.msgSize.
func randMsg(m []byte, generator rand.Rand) {
	for i := range m {
		m[i] = chars[generator.Intn(len(chars))]
	}
}

// Calculates aggregate raw message output in human / network units.
func calcOutput(n uint64) (float64, string) {
	m := (float64(n) / 5) * float64(Config.msgSize)
	var o string
	switch {
	case m >= 131072:
		o = strconv.FormatFloat(m/131072, 'f', 0, 64) + "Mb/sec"
	case m < 131072:
		o = strconv.FormatFloat(m/1024, 'f', 0, 64) + "KB/sec"
	}
	return m, o
}
