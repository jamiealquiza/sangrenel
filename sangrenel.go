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
	sig_chan        = make(chan os.Signal)
	clientKill_chan = make(chan bool, 24)
	brokers         []string
	topic           *string
	msgSize         *int
	msgRate         *int
	clientWorkers   *int
	noop            *bool
	chars           = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890!@#$%^&*(){}][:<>.")
	sentCntr        = make(chan int, 1)
	latency         []float64
	latency_chan    = make(chan float64, 1)
	resetLat_chan   = make(chan bool, 1)
)

func init() {
	flag_brokers := flag.String("brokers", "localhost:9092", "Comma delimited list of Kafka brokers")
	topic = flag.String("topic", "sangrenel", "Topic to publish to")
	msgSize = flag.Int("size", 300, "Message size in bytes")
	msgRate = flag.Int("rate", 100000000, "Apply a global message rate limit")
	noop = flag.Bool("noop", false, "Test message generation performance, do not transmit messages")
	clientWorkers = flag.Int("workers", 1, "Number of Kafka client workers")
	flag.Parse()
	brokers = strings.Split(*flag_brokers, ",")
	sentCntr <- 0
	runtime.GOMAXPROCS(runtime.NumCPU())
}

func incrSent() {
	i := <-sentCntr
	sentCntr <- i + 1
}

func fetchSent() int {
	i := <-sentCntr
	sentCntr <- i
	return i
}

func fetchResetSent() int {
	i := <-sentCntr
	sentCntr <- 0
	return i
}

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

func randMsg(m []rune, generator *rand.Rand) string {
	for i := range m {
		m[i] = chars[generator.Intn(len(chars))]
	}
	return string(m)
}

func sendWorker(c kafka.Client) {
	producer, err := kafka.NewProducer(&c, nil)
	if err != nil {
		fmt.Println(err.Error())
	}
	defer producer.Close()

	source := rand.NewSource(time.Now().UnixNano())
	generator := rand.New(source)
	msg := make([]rune, *msgSize)
	switch *noop {
	case true:
		for {
			randMsg(msg, generator)
			incrSent()
		}
	default:
		for {
			rateStart := time.Now().Add(time.Second)
			countStart := fetchSent()
			var start time.Time
			for fetchSent()-countStart < *msgRate {
				data := randMsg(msg, generator)
				start = time.Now()
				err = producer.SendMessage(*topic,
					nil,
					kafka.StringEncoder(data))
				if err != nil {
					fmt.Println(err)
				} else {
					incrSent()
					latency_chan <- time.Since(start).Seconds() * 1000
				}
			}
			time.Sleep(rateStart.Sub(time.Now()) + time.Since(start))
		}
	}
}

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
	fmt.Printf("%s shutting down\n", cId)
	client.Close()
}

func calcOutput(n int) string {
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
	signal.Notify(sig_chan, syscall.SIGINT, syscall.SIGTERM)
	go latencyAggregator()
	fmt.Printf("\n::: Sangrenel :::\nStarting %s workers\nMessage size %s bytes\n\n",
		strconv.Itoa(*clientWorkers),
		strconv.Itoa(*msgSize))
	for i := 0; i < *clientWorkers; i++ {
		go createClient(i + 1)
	}
	tick := time.Tick(5 * time.Second)
	for {
		select {
		case <-tick:
			sentCnt := fetchResetSent()
			fmt.Printf("%s Generating %s @ %d messages/sec | topic: %s | %.2fms avg latency\n",
				time.Now().Format(time.RFC3339),
				calcOutput(sentCnt),
				sentCnt/5,
				*topic,
				calcLatency())
		case <-sig_chan:
			fmt.Println()
			for i := 0; i < *clientWorkers; i++ {
				clientKill_chan <- true
			}
			close(clientKill_chan)
			time.Sleep(2 * time.Second)
			os.Exit(0)
		}
	}
}
