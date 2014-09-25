package main

import (
	"flag"
	"fmt"
	kafka "github.com/Shopify/sarama"
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
	clientWorkers   *int
	sentCounter     int
	message         = "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Morbi eu volutpat mauris, a aliquam dolor. Sed non ultrices odio, vel aliquam quam. Pellentesque ut elit eget sem pretium suscipit a sed tortor. Duis sit amet cursus risus. Nullam imperdiet hendrerit dapibus. Cras non rutrum arcu. Etiam fringilla faucibus euismod. Fusce rhoncus orci risus, vel porttitor arcu pulvinar nec. Cras id neque a tortor aliquam efficitur. Nunc egestas nec nunc molestie elementum. Integer nec magna quis ante tempus placerat. Praesent scelerisque ante vel placerat efficitur. Praesent efficitur eleifend enim, id vehicula risus volutpat sed. Sed porta est ac risus feugiat, non sollicitudin nulla lobortis. Aliquam pulvinar molestie ullamcorper. Morbi ante magna, porta ac malesuada gravida, eleifend gravida felis."
)

func init() {
	flag_brokers := flag.String("brokers", "localhost:9092", "Comma delimited list of Kafka brokers")
	topic = flag.String("topic", "sangrenel", "Topic to publish to")
	clientWorkers = flag.Int("workers", 1, "Number of Kafka client workers")
	flag.Parse()
	brokers = strings.Split(*flag_brokers, ",")
}

func sendWorker(c kafka.Client) {
	producer, err := kafka.NewProducer(&c, nil)
	if err != nil {
		panic(err)
	}
	defer producer.Close()

	for {
		err = producer.SendMessage(*topic, nil, kafka.StringEncoder(message))
		if err != nil {
			panic(err)
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
	fmt.Printf("\n::: Sangrenel :::\nStarting %s workers\n\n", strconv.Itoa(*clientWorkers))
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
			for i := 0; i < *clientWorkers; i++ {
				clientKill_chan <- true
			}
			time.Sleep(3 * time.Second)
			os.Exit(0)
		}
	}
}
