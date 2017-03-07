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
	"bufio"
	"crypto/tls"
	"crypto/x509"
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
	tlsconfig      *tls.Config

	source MessageSource

	// Character selection for random messages.
	chars = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890!@#$^&*(){}][:<>.")

	// Counters / misc.
	signals     = make(chan os.Signal)
	killClients = make(chan bool, 24)
	sentCntr    = make(chan int64, 1)
)

type MessageSource interface {
	PutMessage(buffer []byte) []byte
	Clone() MessageSource
}

type RandomMessageSource struct {
	generator *rand.Rand
}

func NewRandomMessageSource() *RandomMessageSource {
	source := rand.NewSource(time.Now().UnixNano())
	return &RandomMessageSource{
		generator: rand.New(source),
	}
}

func (source *RandomMessageSource) PutMessage(buffer []byte) []byte {
	for i := range buffer {
		buffer[i] = chars[source.generator.Intn(len(chars))]
	}
	return buffer
}

func (source *RandomMessageSource) Clone() MessageSource {
	return source
}

type ReplayMessageSource struct {
	lines [][]byte
	index int
}

func NewReplayMessageSource(path string) (*ReplayMessageSource, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Could not open data file %s for replay: %v", path, err)
	}

	lines := make([][]byte, 0, 100)
	scanner := bufio.NewScanner(handle)
	for scanner.Scan() {
		lines = append(lines, scanner.Bytes())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("Error reading from data file %s: %v", path, err)
	}

	return &ReplayMessageSource{
		lines: lines,
		index: 0,
	}, nil
}

func (source *ReplayMessageSource) Clone() MessageSource {
	return &ReplayMessageSource{
		lines: source.lines,
		index: source.index,
	}
}

func (source *ReplayMessageSource) PutMessage(buffer []byte) []byte {
	if source.index >= len(source.lines) {
		source.index = 0
	}
	line := source.lines[source.index]
	buffer = buffer[:len(line)]
	for i := range line {
		buffer[i] = line[i]
	}
	source.index++
	return buffer
}

func init() {
	flag.StringVar(&topic, "topic", "sangrenel", "Topic to publish to")
	flag.IntVar(&msgSize, "size", 300, "Message size in bytes")
	flag.Int64Var(&msgRate, "rate", 100000000, "Apply a global message rate limit")
	flag.IntVar(&batchSize, "batch", 0, "Max messages per batch. Defaults to unlimited (0).")
	flag.StringVar(&compressionOpt, "compression", "none", "Message compression: none, gzip, snappy")
	flag.BoolVar(&noop, "noop", false, "Test message generation performance, do not transmit messages")
	flag.IntVar(&clients, "clients", 1, "Number of Kafka client workers")
	flag.IntVar(&producers, "producers", 5, "Number of producer instances per client")
	dataPath := flag.String("data", "", "File of lines that each producer should send to the broker")
	brokerString := flag.String("brokers", "localhost:9092", "Comma delimited list of Kafka brokers")
	clientCertPath := flag.String("cert", "", "Path to TLS client certificate in PEM format")
	clientKeyPath := flag.String("key", "", "Path to TLS client private key in PEM format")
	caPath := flag.String("ca", "", "Path to CA root certificate in PEM format")
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

	// Select the proper message source based on command line options.
	if len(*dataPath) == 0 {
		fmt.Printf("Writing random strings of %d bytes.\n", msgSize)
		source = NewRandomMessageSource()
	} else {
		fmt.Printf("Writing data from %s.\n", *dataPath)
		var err error
		source, err = NewReplayMessageSource(*dataPath)
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}
	}

	// Build TLS configuration if command line options are specified.
	hasCert := len(*clientCertPath) > 0
	hasKey := len(*clientKeyPath) > 0
	hasCA := len(*caPath) > 0
	if (hasCert || hasKey || hasCA) != (hasCert && hasKey && hasCA) {
		fmt.Printf("Must specify all three of cert, key, and ca, or none.\n")
		os.Exit(1)
	} else if hasCert { // Build TLS config
		cert, err := tls.LoadX509KeyPair(*clientCertPath, *clientKeyPath)
		if err != nil {
			fmt.Printf("Failed to load key pair from cert file %s and key file %s: %v\n", *clientCertPath, *clientKeyPath, err)
			os.Exit(1)
		}

		h, err := os.Open(*caPath)
		if err != nil {
			fmt.Printf("Could not open CA %s: %v\n", *caPath, err)
			os.Exit(1)
		}
		defer h.Close()
		fi, err := h.Stat()
		if err != nil {
			fmt.Printf("Could not stat %s: %v\n", *caPath, err)
			os.Exit(1)
		}
		certBuffer := make([]byte, fi.Size())
		n, err := h.Read(certBuffer)
		if err != nil {
			fmt.Printf("Could not read from %s: %v\n", *caPath, err)
			os.Exit(1)
		}
		if n != int(fi.Size()) {
			fmt.Printf("Bytes read didn't match file size in %s: expected %d, read %d\n", *caPath, fi.Size(), n)
			os.Exit(1)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(certBuffer) {
			fmt.Printf("No certs found in %s.\n", *caPath)
			os.Exit(1)
		}

		tlsconfig = &tls.Config{
			InsecureSkipVerify: true,
			RootCAs:            pool,
			Certificates:       []tls.Certificate{cert},
		}
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

	localSource := source.Clone()
	msgData := make([]byte, msgSize)

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
		countStart := fetchSent()
		var start time.Time
		for fetchSent()-countStart < msgRate {
			msgData := localSource.PutMessage(msgData)
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
func clientDummyProducer(t *tachymeter.Tachymeter) {
	localSource := source.Clone()
	msg := make([]byte, msgSize)

	var n int64
	var times [10]time.Duration

	for {
		start := time.Now()
		msg = localSource.PutMessage(msg)

		// Increment global counter and
		// tachymeter every 10 messages.
		n++
		times[n-1] = time.Since(start)
		if n == 10 {
			incrSent(10)
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

		if tlsconfig != nil {
			conf.Net.TLS.Enable = true
			conf.Net.TLS.Config = tlsconfig
		}

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
			go clientDummyProducer(t)
		}
	}
	<-killClients
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

	t := tachymeter.New(&tachymeter.Config{Size: 300000, Safe: true})

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
