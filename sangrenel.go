package main

import (
	"flag"
	"fmt"
	kafka "github.com/Shopify/sarama"
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	sig_chan        = make(chan os.Signal)
	clientKill_chan = make(chan bool, 1)
	brokers         []string
	topic           *string
	msgSize         *int
	clientWorkers   *int
	noop            *bool
	sentCounter     int
	chars           = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890!@#$%^&*(){}][:<>.")
)

func init() {
	flag_brokers := flag.String("brokers", "localhost:9092", "Comma delimited list of Kafka brokers")
	topic = flag.String("topic", "sangrenel", "Topic to publish to")
	msgSize = flag.Int("size", 300, "Message size in bytes")
	noop = flag.Bool("noop", false, "Test message generation performance, do not transmit messages")
	clientWorkers = flag.Int("workers", 1, "Number of Kafka client workers")
	flag.Parse()
	brokers = strings.Split(*flag_brokers, ",")
	runtime.GOMAXPROCS(runtime.NumCPU())
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
			sentCounter++
		}
	default:
		for {
			err = producer.SendMessage(*topic, nil, kafka.StringEncoder(randMsg(msg, generator)))
			if err != nil {
				fmt.Println(err.Error())
			} else {
				sentCounter++
			}
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
	case m > 131072:
		o = strconv.FormatFloat(m/131072, 'f', 0, 64) + "Mb/sec"
	case m < 131072:
		o = strconv.FormatFloat(m/1024, 'f', 0, 64) + "KB/sec"
	}
	return o
}

func main() {
	signal.Notify(sig_chan, syscall.SIGINT, syscall.SIGTERM)
	fmt.Printf("\n::: Sangrenel :::\nStarting %s workers\nMessage size %s bytes\n\n", strconv.Itoa(*clientWorkers), strconv.Itoa(*msgSize))
	for i := 0; i < *clientWorkers; i++ {
		go createClient(i + 1)
	}
	tick := time.Tick(5 * time.Second)
	for {
		select {
		case <-tick:
			fmt.Printf("%s Producing %s raw data @ %d messages/sec - topic: %s\n",
				time.Now().Format(time.RFC3339),
				calcOutput(sentCounter),
				sentCounter/5,
				*topic)
			sentCounter = 0
		case <-sig_chan:
			fmt.Println()
			for i := 0; i < *clientWorkers; i++ {
				clientKill_chan <- true
			}
			time.Sleep(3 * time.Second)
			os.Exit(0)
		}
	}
}
