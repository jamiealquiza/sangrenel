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
	"strconv"
	"strings"
	"syscall"
	"time"

	kafka "github.com/Shopify/sarama"
	"github.com/jamiealquiza/tachymeter"
)

var (
	// Configs.
	brokers        []string
	topic          string
	msgSize        int
	msgRate        int64
	batchSize      int
	compressionOpt string
	compression    kafka.CompressionCodec
	clients        int
	producers      int
	noop           bool

	// Character selection for random messages.
	chars = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890!@#$^&*(){}][:<>.")

	// Counters / misc.
	signals     = make(chan os.Signal)
	killClients = make(chan bool, 24)
	sentCntr    = make(chan int64, 1)
)

func init() {
	flag.StringVar(&topic, "topic", "sangrenel", "Topic to publish to")
	flag.IntVar(&msgSize, "size", 300, "Message size in bytes")
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
}

// clientProducer generates random messages and writes to Kafka.
// Workers track and limit message rates using incrSent() and fetchSent().
// Default 5 instances of clientProducer are created under each Kafka client.
func clientProducer(c kafka.Client, t *tachymeter.Tachymeter) {
	producer, err := kafka.NewSyncProducerFromClient(c)
	if err != nil {
		log.Println(err.Error())
	}
	defer producer.Close()

	source := rand.NewSource(time.Now().UnixNano())
	generator := rand.New(source)
	msgData := make([]byte, msgSize)

	// Use a local accumulator then periodically update global counter.
	// Global counter can become a bottleneck with too many threads.
	//tick := time.Tick(2 * time.Millisecond)
	var n int64
	var times [10]time.Duration

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
					incrSent(10)
					t.AddCount(10)
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
func kafkaClient(n int, t *tachymeter.Tachymeter) {
	switch noop {
	// If not noop, actually fire up Kafka connections and send messages.
	case false:
		cId := "client_" + strconv.Itoa(n)

		conf := kafka.NewConfig()
		if compression != kafka.CompressionNone {
			conf.Producer.Compression = compression
		}
		conf.Producer.Flush.MaxMessages = batchSize

		conf.Producer.MaxMessageBytes = msgSize + 50

		client, err := kafka.NewClient(brokers, conf)
		if err != nil {
			log.Println(err)
			os.Exit(1)
		} else {
			log.Printf("%s connected\n", cId)
		}
		for i := 0; i < producers; i++ {
			go clientProducer(client, t)
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

// Global counter functions.
func incrSent(n int64) {
	i := <-sentCntr
	sentCntr <- i + n
}
func fetchSent() int64 {
	i := <-sentCntr
	sentCntr <- i
	return i
}

// Calculates aggregate raw message output in human / network units.
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

func main() {
	// Listens for signals.
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	if graphiteIp != "" {
		go graphiteWriter()
	}

	// Print Sangrenel startup info.
	fmt.Println("\n::: Sangrenel :::")
	fmt.Printf("\nStarting %d client workers, %d producers per worker\n", clients, producers)
	fmt.Printf("Message size %d bytes, %d message limit per batch\n", msgSize, batchSize)
	switch compressionOpt {
	case "none":
		fmt.Println("Compression: none")
	case "gzip":
		fmt.Println("Compression: GZIP")
	case "snappy":
		fmt.Println("Compression: Snappy")
	}

	t := tachymeter.New(&tachymeter.Config{Size: 1000000, Safe: true})

	// Start client workers.
	for i := 0; i < clients; i++ {
		go kafkaClient(i+1, t)
	}

	// Start Sangrenel periodic info output.
	tick := time.Tick(5 * time.Second)

	var currCnt, lastCnt int64
	start := time.Now()
	for {
		select {
		case <-tick:
			// Set tachymeter wall time.
			t.SetWallTime(time.Since(start))

			// Set last and current to last read sent count.
			lastCnt = currCnt

			// Get actual current sent count, then delta from last count.
			// Delta is divided by update interval (5s) for per-second rate over a window.
			currCnt = fetchSent()
			deltaCnt := currCnt - lastCnt

			stats := t.Calc()
			t.Reset()

			outputBytes, outputString := calcOutput(deltaCnt)

			// Update the metrics map for the Graphite writer.
			metrics["rate"] = stats.Rate.Second
			metrics["output"] = outputBytes
			metrics["5p"] = (float64(stats.Time.Long5p.Nanoseconds()) / 1000) / 1000
			// Add ts for Graphite.
			now := time.Now()
			ts := float64(now.Unix())
			metrics["timestamp"] = ts

			if graphiteIp != "" {
				metricsOutgoing <- metrics
			}

			fmt.Println()
			log.Printf("Generating %s @ %.0f messages/sec | topic: %s | %.2fms top 5%% latency\n",
				outputString,
				metrics["rate"],
				topic,
				metrics["5p"])

			stats.Dump()

			// Reset interval time.
			start = time.Now()

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
