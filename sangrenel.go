package main

import (
	"flag"
	"fmt"
	kafka "github.com/Shopify/sarama"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	sig_chan        = make(chan os.Signal)
	clientKill_chan = make(chan bool, 1)
	brokers         = []string{}
	topic           *string
	msgSize         *int
	clientWorkers   *int
	sentCounter     int
	chars           = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890!@#$%^&*(){}][:<>.")
)

func init() {
	flag_brokers := flag.String("brokers", "localhost:9092", "Comma delimited list of Kafka brokers")
	topic = flag.String("topic", "sangrenel", "Topic to publish to")
	msgSize = flag.Int("size", 300, "Message size in bytes")
	clientWorkers = flag.Int("workers", 1, "Number of Kafka client workers")
	flag.Parse()
	brokers = strings.Split(*flag_brokers, ",")
	rand.Seed(time.Now().UnixNano())
}

func randMsg(n int) string {
	s := make([]rune, n)
	for i := range s {
		s[i] = chars[rand.Intn(len(chars))]
	}
	return string(s)
}

func sendWorker(c kafka.Client) {
	producer, err := kafka.NewProducer(&c, nil)
	if err != nil {
		panic(err)
	}
	defer producer.Close()
	for {
		err = producer.SendMessage(*topic, nil, kafka.StringEncoder(randMsg(*msgSize)))
		if err != nil {
			fmt.Println(err.Error())
		} else {
			sentCounter++
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
			fmt.Printf("%d messages/sec published to topic: %s\n", sentCounter/5, *topic)
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
