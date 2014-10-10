package main

import (
	"flag"
	"fmt"
	kafka "github.com/Shopify/sarama"
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	// Control chans
	sig_chan        = make(chan os.Signal)
	clientKill_chan = make(chan bool, 24)
	// Init config vars
	brokers       []string
	topic         *string
	msgSize       *int
	msgRate       *int64
	clientWorkers *int
	noop          *bool
	// Character selection from which random messages are generated
	chars = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890!@#$%^&*(){}][:<>.")
	// Counters / misc.
	sentCntr      = make(chan int64, 1)
	latency       []float64
	latency_chan  = make(chan float64, 1)
	resetLat_chan = make(chan bool, 1)
)

func init() {
	flag_brokers := flag.String("brokers", "localhost:9092", "Comma delimited list of Kafka brokers")
	topic = flag.String("topic", "sangrenel", "Topic to publish to")
	msgSize = flag.Int("size", 300, "Message size in bytes")
	msgRate = flag.Int64("rate", 100000000, "Apply a global message rate limit")
	noop = flag.Bool("noop", false, "Test message generation performance, do not transmit messages")
	clientWorkers = flag.Int("workers", 1, "Number of Kafka client workers")
	flag.Parse()

	brokers = strings.Split(*flag_brokers, ",")
	// Init sent count at 0
	sentCntr <- 0
	runtime.GOMAXPROCS(runtime.NumCPU())
}

// Returns a random message generated from
// the 'chars' rune set, of 'm' length in bytes as defined
// by ''*msgSize'
func randMsg(m []rune, generator *rand.Rand) string {
	for i := range m {
		m[i] = chars[generator.Intn(len(chars))]
	}
	return string(m)
}

// Thread-safe global counter function via
// buffered channel with capacity of 1
func incrSent() {
	i := <-sentCntr
	sentCntr <- i + 1
}

// Fetch counter val from channel
// then reload into buffer
func fetchSent() int64 {
	i := <-sentCntr
	sentCntr <- i
	return i
}

// A producer instance of a parent Kafka client connection
func sendWorker(c kafka.Client) {
	producer, err := kafka.NewProducer(&c, nil)
	if err != nil {
		fmt.Println(err.Error())
	}
	defer producer.Close()

	// Instantiate 'rand' per producer to avoid
	// mutex contention of a globally shared object
	source := rand.NewSource(time.Now().UnixNano())
	generator := rand.New(source)
	// 'msg' object reuse
	msg := make([]rune, *msgSize)

	switch *noop {
	case true:
		for {
			randMsg(msg, generator)
			incrSent()
		}
	default:
		for {
			// Message rate limit works by having
			// all producer worker loops incrementing
			// a global counter and tracking
			// the aggregate per-second progress.
			// If the configured rate is met, the worker will sleep
			// for the remainder of the 1 second window.
			rateStart := time.Now().Add(time.Second)
			countStart := fetchSent()
			// 'start' is a time marker to track ack latency
			var start time.Time
			for fetchSent()-countStart < *msgRate {
				// Gen a random message first,
				data := randMsg(msg, generator)
				// then start the latency clock to ensure
				// transmit -> broker ack time is metered
				start = time.Now()
				err = producer.SendMessage(*topic,
					nil,
					kafka.StringEncoder(data))
				if err != nil {
					fmt.Println(err)
				} else {
					// Increment global sent count and fire off
					// time delta since 'start' into the latency chan
					incrSent()
					latency_chan <- time.Since(start).Seconds() * 1000
				}
			}
			// If the global per-second rate limit was met, the inner loop
			// breaks and the outer loop sleeps for the second remainder
			time.Sleep(rateStart.Sub(time.Now()) + time.Since(start))
		}
	}
}

// A connection to a Kafka cluster, manages 5
// 'sendworker()' (producer instances); fixed value since
// these tend to flatline throughput at ~5 producers
func createClient(n int) {
	cId := "client_" + strconv.Itoa(n)
	client, err := kafka.NewClient(cId, brokers, kafka.NewClientConfig())
	if err != nil {
		panic(err)
	} else {
		fmt.Printf("%s connected\n", cId)
	}

	for i := 0; i < 5; i++ {
		go sendWorker(*client)
	}
	<-clientKill_chan
	// We don't gracefully close producers
	// so we don't have to wait for a large
	// number of in-flight messages to complete
	client.Close()
}

// Calculates raw message output in
// networking friendly units; gives an idea of
// minimum network traffic being generated
func calcOutput(n int64) string {
	m := (float64(n) / 5) * float64(*msgSize)
	var o string
	switch {
	case m >= 131072:
		o = strconv.FormatFloat(m/131072, 'f', 0, 64) + "Mb/sec"
	case m < 131072:
		o = strconv.FormatFloat(m/1024, 'f', 0, 64) + "KB/sec"
	}
	return o
}

// Thread-safe receiver for latency values
// captured by all producer goroutines.
// May want to do something smart about this
// (e.g. sampling) to limit the time-complexity on
// sorting huge slices in high-throughput configurations.
func latencyAggregator() {
	for {
		select {
		case i := <-latency_chan:
			latency = append(latency, i)
		case <-resetLat_chan:
			latency = latency[:0]
		}
	}
}

// Fetches & resets current latencies set
// populated by 'latencyAggregator()'.
// Sorts then averages the 90th percentile
// worse latencies.
func calcLatency() float64 {
	var avg float64
	switch *noop {
	case true:
		break
	default:
		lat := latency
		resetLat_chan <- true
		sort.Float64s(lat)
		var sum float64
		topn := int(float64(len(lat)) * 0.90)
		for i := topn; i < len(lat); i++ {
			sum += lat[i]
		}
		avg = sum / float64(len(lat)-topn)
	}
	return avg
}

func main() {
	// Listens for signals
	signal.Notify(sig_chan, syscall.SIGINT, syscall.SIGTERM)
	// Fires up 'latencyAggregator()' to background
	go latencyAggregator()

	// Warns you stuff is happening
	fmt.Printf("\n::: Sangrenel :::\nStarting %s workers\nMessage size %s bytes\n\n",
		strconv.Itoa(*clientWorkers),
		strconv.Itoa(*msgSize))
	// Fires up clients
	for i := 0; i < *clientWorkers; i++ {
		go createClient(i + 1)
	}

	// Info output ticker
	tick := time.Tick(5 * time.Second)
	// Markers for tracking message rates
	var currCnt, lastCnt int64
	for {
		select {
		case <-tick:
			lastCnt = currCnt
			currCnt = fetchSent()
			deltaCnt := currCnt - lastCnt
			fmt.Printf("%s Generating %s @ %d messages/sec | topic: %s | %.2fms avg latency\n",
				// Always be RFC'ing
				time.Now().Format(time.RFC3339),
				calcOutput(deltaCnt),
				deltaCnt/5,
				*topic,
				// Well, this technically appends a
				// latency to the 5s interval.
				calcLatency())
		// The Chuck Norris of signals; it doesn't sleep, it waits
		case <-sig_chan:
			fmt.Println("Killing Connections")
			for i := 0; i < *clientWorkers; i++ {
				clientKill_chan <- true
			}
			close(clientKill_chan)
			time.Sleep(2 * time.Second)
			os.Exit(0)
		}
	}
}
