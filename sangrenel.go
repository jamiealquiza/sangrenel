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
	"math"
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
	brokers          []string
	topic            string
	msgSize          int
	msgRate          uint64
	batchSize        int
	compression      sarama.CompressionCodec
	workers          int
	writersPerWorker int
	noop             bool
}

var (
	Config = &config{}

	// Character selection for random messages.
	chars = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890!@#$^&*(){}][:<>.")

	// Counters / misc.
	sentCnt uint64
	errCnt  uint64
)

func init() {
	flag.StringVar(&Config.topic, "topic", "sangrenel", "Kafka topic to produce to")
	flag.IntVar(&Config.msgSize, "message-size", 300, "Message size (bytes)")
	flag.Uint64Var(&Config.msgRate, "produce-rate", 100000000, "Global write rate limit (messages/sec)")
	flag.IntVar(&Config.batchSize, "message-batch-size", 1, "Messages per batch")
	compression := flag.String("compression", "none", "Message compression: none, gzip, snappy")
	flag.BoolVar(&Config.noop, "noop", false, "Test message generation performance (does not connect to Kafka)")
	flag.IntVar(&Config.workers, "workers", 1, "Number of workers")
	flag.IntVar(&Config.writersPerWorker, "writers-per-worker", 5, "Number of writer (Kafka producer) goroutines per worker")
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
	fmt.Printf("\nStarting %d client workers, %d writers per worker\n", Config.workers, Config.writersPerWorker)
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
		go worker(i+1, t)
	}

	var currSentCnt, lastSentCnt uint64
	var currErrCnt, lastErrCnt uint64

	interval := 5 * time.Second
	ticker := time.Tick(interval)
	start := time.Now()

	for {
		<-ticker

		intervalTime := time.Since(start).Seconds()

		// Set tachymeter wall time.
		t.SetWallTime(time.Since(start))

		// Get the sent count from the last interval, then the delta
		// (sentSinceLastInterval) between the current and last interval.
		lastSentCnt = currSentCnt
		currSentCnt = atomic.LoadUint64(&sentCnt)
		sentSinceLastInterval := currSentCnt - lastSentCnt

		outputBytes, outputString := calcOutput(intervalTime, sentSinceLastInterval)

		// Update error counters.
		lastErrCnt = currErrCnt
		currErrCnt = atomic.LoadUint64(&errCnt)
		errSinceLastInterval := currErrCnt - lastErrCnt

		errRate := (float64(errSinceLastInterval) / float64(sentSinceLastInterval)) * 100

		// Summarize tachymeter data.
		stats := t.Calc()

		// Update the metrics map for the Graphite writer.
		metrics["rate"] = float64(sentSinceLastInterval) / intervalTime
		metrics["error_rate"] = errRate
		metrics["output"] = outputBytes
		metrics["p99"] = (float64(stats.Time.P99.Nanoseconds()) / 1000) / 1000
		metrics["timestamp"] = float64(time.Now().Unix())
		// Ship metrics if configured.
		if graphiteIp != "" {
			metricsOutgoing <- metrics
		}

		// Write output stats.
		fmt.Println()
		log.Printf("[ topic: %s ]\n", Config.topic)
		fmt.Printf("> Producing %s @ %.0f msgs/sec. | %.2fms p99 batch write | error rate %.2f%%\n",
			outputString,
			metrics["rate"],
			//Config.topic,
			metrics["p99"],
			metrics["error_rate"])

		if !Config.noop {
			fmt.Printf("> Batch Statistics, Last %.1fs:\n", intervalTime)
			stats.Dump()

			// Check if the tacymeter size needs to be increased
			// to avoid sampling. Otherwise, just reset it.
			if int(sentSinceLastInterval) > len(t.Times) {
				newTachy := tachymeter.New(&tachymeter.Config{Size: int(2 * sentSinceLastInterval)})
				// This is actually dangerous;
				// this could swap in a tachy with unlocked
				// mutexes while the current one has locks held.
				*t = *newTachy
			} else {
				t.Reset()
			}
		}

		// Reset interval time.
		start = time.Now()
	}
}

// worker is a high level producer unit and holds a single
// Kafka client. The worker's Kafka client is shared by n (Config.writersPerWorker)
// writer instances that perform the message generation and writing.
func worker(n int, t *tachymeter.Tachymeter) {
	switch Config.noop {
	case false:
		cId := "worker_" + strconv.Itoa(n)

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

		for i := 0; i < Config.writersPerWorker; i++ {
			go writer(client, t)
		}
	// If noop, we're not creating connections at all.
	// Just generate messages and burn CPU.
	default:
		for i := 0; i < Config.writersPerWorker; i++ {
			go dummyWriter(t)
		}
	}

	wait := make(chan bool)
	<-wait
}

// writer generates random messages and write to Kafka.
// Each wrtier belongs to a parent worker. Writers
// throttle writes according to a global rate limiter
// and report write throughput statistics up through
// a shared tachymeter.
func writer(c sarama.Client, t *tachymeter.Tachymeter) {
	// Init the producer.
	producer, err := sarama.NewSyncProducerFromClient(c)
	if err != nil {
		log.Println(err.Error())
	}
	defer producer.Close()

	source := rand.NewSource(time.Now().UnixNano())
	generator := rand.New(source)
	msgBatch := make([]*sarama.ProducerMessage, 0, Config.batchSize)

	for {
		// Message rate limiting works by having all writer loops incrementing
		// a global counter and tracking the aggregate per-second progress.
		// If the configured rate is met, the worker will sleep
		// for the remainder of the 1 second window.
		intervalEnd := time.Now().Add(time.Second)
		countStart := atomic.LoadUint64(&sentCnt)

		var sendTime time.Time

		for {
			// Break if the global rate limit was met, or, if
			// we'd exceed it assuming all writers wrote a max batch size
			// for this interval.
			intervalSent := atomic.LoadUint64(&sentCnt) - countStart
			sendEstimate := intervalSent + uint64(Config.batchSize*Config.workers*(Config.writersPerWorker-1))
			if sendEstimate >= Config.msgRate {
				break
			}

			// Estimate the batch size. This should shrink
			// if we're near the rate limit. Estimated batch size =
			// amount left to send for this interval / number of writers
			// we have available to send this amount. If the estimate
			// is lower than the configured batch size, send that amount
			// instead.
			toSend := (Config.msgRate - intervalSent) / uint64((Config.workers * Config.writersPerWorker))
			n := int(math.Min(float64(toSend), float64(Config.batchSize)))

			for i := 0; i < n; i++ {
				// Gen message.
				msgData := make([]byte, Config.msgSize)
				randMsg(msgData, *generator)
				msg := &sarama.ProducerMessage{Topic: Config.topic, Value: sarama.ByteEncoder(msgData)}
				// Append to batch.
				msgBatch = append(msgBatch, msg)
			}

			sendTime = time.Now()
			err = producer.SendMessages(msgBatch)
			if err != nil {
				// Sarama returns a ProducerErrors, which is a slice
				// of errors per message errored. Use this count
				// to establish an error rate.
				atomic.AddUint64(&errCnt, uint64(len(err.(sarama.ProducerErrors))))
			}

			t.AddTime(time.Since(sendTime))
			atomic.AddUint64(&sentCnt, uint64(len(msgBatch)))

			msgBatch = msgBatch[:0]
		}

		// If the global per-second rate limit was met,
		// the inner loop breaks and the outer loop sleeps for the interval remainder.
		time.Sleep(intervalEnd.Sub(time.Now()))
	}
}

// dummyWriter is initialized by the worker(s) if Config.noop is True.
// dummyWriter performs the message generation step of the normal writer,
// but doesn't connect to / attempt to send anything to Kafka. This is used
// purely for testing message generation performance.
func dummyWriter(t *tachymeter.Tachymeter) {
	source := rand.NewSource(time.Now().UnixNano())
	generator := rand.New(source)
	msgBatch := make([]*sarama.ProducerMessage, 0, Config.batchSize)

	for {
		for i := 0; i < Config.batchSize; i++ {
			// Gen message.
			msgData := make([]byte, Config.msgSize)
			randMsg(msgData, *generator)
			msg := &sarama.ProducerMessage{Topic: Config.topic, Value: sarama.ByteEncoder(msgData)}
			// Append to batch.
			msgBatch = append(msgBatch, msg)
		}

		atomic.AddUint64(&sentCnt, uint64(len(msgBatch)))
		t.AddTime(time.Duration(0))

		msgBatch = msgBatch[:0]
	}
}

// randMsg returns a random message generated from the chars byte slice.
// Message length of m bytes as defined by Config.msgSize.
func randMsg(m []byte, generator rand.Rand) {
	for i := range m {
		m[i] = chars[generator.Intn(len(chars))]
	}
}

// calcOutput takes a duration t and messages sent
// and returns message rates in human readable network speeds.
func calcOutput(t float64, n uint64) (float64, string) {
	m := (float64(n) / t) * float64(Config.msgSize)
	var o string
	switch {
	case m >= 131072:
		o = strconv.FormatFloat(m/131072, 'f', 0, 64) + "Mb/sec"
	case m < 131072:
		o = strconv.FormatFloat(m/1024, 'f', 0, 64) + "KB/sec"
	}
	return m, o
}
