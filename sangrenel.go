package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Shopify/sarama"
	"github.com/jamiealquiza/tachymeter"
)

type config struct {
	brokers               []string
	topic                 string
	msgSize               int
	msgRate               uint64
	batchSize             int
	compression           sarama.CompressionCodec
	compressionName       string
	requiredAcks          sarama.RequiredAcks
	requiredAcksName      string
	maxOpenRequests       int
	workers               int
	writersPerWorker      int
	noop                  bool
	emitErrors            bool
	interval              int
	kafkaVersion          sarama.KafkaVersion
	kafkaVersionString    string
	tls                   bool
	tlsCaCertificate      string
	tlsCertificate        string
	tlsPrivateKey         string
	tlsInsecureSkipVerify bool
}

var (
	Config = &config{}

	// Character selection for random messages.
	chars = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890!@#$^&*(){}][:<>.")

	// Counters / misc.
	sentCnt uint64
	errCnt  uint64

	validVersions = []string{
		"0.8.2.0",
		"0.8.2.1",
		"0.8.2.2",
		"0.9.0.0",
		"0.9.0.1",
		"0.10.0.0",
		"0.10.0.1",
		"0.10.1.0",
		"0.10.1.1",
		"0.10.2.0",
		"0.10.2.1",
		"0.10.2.2",
		"0.11.0.0",
		"0.11.0.1",
		"0.11.0.2",
		"1.0.0",
		"1.0.1",
		"1.0.2",
		"1.1.0",
		"1.1.1",
		"2.0.0",
		"2.0.1",
		"2.1.0",
		"2.1.1",
		"2.2.0",
		"2.2.1",
		"2.2.2",
		"2.3.0",
		"2.3.1",
		"2.4.0",
		"2.4.1",
		"2.5.0",
		"2.5.1",
		"2.6.0",
		"2.6.1",
		"2.6.2",
		"2.7.0",
		"2.7.1",
		"2.8.0",
		"2.8.1",
		"3.0.0",
		"3.1.0"}
)

func init() {
	flag.StringVar(&Config.topic, "topic", "sangrenel", "Kafka topic to produce to")
	flag.IntVar(&Config.msgSize, "message-size", 300, "Message size (bytes)")
	flag.Uint64Var(&Config.msgRate, "produce-rate", 100000000, "Global write rate limit (messages/sec)")
	flag.IntVar(&Config.batchSize, "message-batch-size", 500, "Messages per batch")
	flag.StringVar(&Config.compressionName, "compression", "none", "Message compression: none, gzip, snappy")
	flag.StringVar(&Config.requiredAcksName, "required-acks", "local", "RequiredAcks config: none, local, all")
	flag.IntVar(&Config.maxOpenRequests, "max-open-requests", 5, "Configures max.in.flight.requests.per.connection")
	flag.BoolVar(&Config.noop, "noop", false, "Test message generation performance (does not connect to Kafka)")
	flag.BoolVar(&Config.emitErrors, "emit-errors", false, "Emit write errors to stderr")
	flag.IntVar(&Config.workers, "workers", 1, "Number of workers")
	flag.IntVar(&Config.writersPerWorker, "writers-per-worker", 5, "Number of writer (Kafka producer) goroutines per worker")
	brokerString := flag.String("brokers", "localhost:9092", "Comma delimited list of Kafka brokers")
	flag.IntVar(&Config.interval, "interval", 5, "Statistics output interval (seconds)")
	flag.StringVar(&Config.kafkaVersionString, "api-version", "", "Explicit sarama.Version string")
	flag.BoolVar(&Config.tls, "tls", false, "Whether to enable TLS communcation")
	flag.StringVar(&Config.tlsCaCertificate, "tls-ca-cert", "", "Path to the CA SSL certificate")
	flag.StringVar(&Config.tlsCertificate, "tls-cert-file", "", "Path to the certificate file")
	flag.StringVar(&Config.tlsPrivateKey, "tls-key-file", "", "Path to the private key file")
	flag.BoolVar(&Config.tlsInsecureSkipVerify, "tls-insecure-skip-verify", false, "TLS insecure skip verify")
	flag.Parse()

	Config.brokers = strings.Split(*brokerString, ",")

	switch Config.compressionName {
	case "gzip":
		Config.compression = sarama.CompressionGZIP
	case "snappy":
		Config.compression = sarama.CompressionSnappy
	case "none":
		Config.compression = sarama.CompressionNone
	default:
		fmt.Printf("Invalid compression option: %s\n", Config.compressionName)
		os.Exit(1)
	}

	switch Config.requiredAcksName {
	case "none":
		Config.requiredAcks = sarama.NoResponse
	case "local":
		Config.requiredAcks = sarama.WaitForLocal
	case "all":
		Config.requiredAcks = sarama.WaitForAll
	default:
		fmt.Printf("Invalid required-acks option: %s\n", Config.requiredAcksName)
		os.Exit(1)
	}

}

func parseKafkaVersion(kafkaVersion string) sarama.KafkaVersion {
	version, err := sarama.ParseKafkaVersion(kafkaVersion)
	if err != nil {
		fmt.Printf("Invalid API version option: %s\n", Config.kafkaVersionString)
		fmt.Printf("Options: %+q\n", validVersions)
		os.Exit(1)
	}
	return version
}

func main() {
	if graphiteIp != "" {
		go graphiteWriter()
	}

	version := Config.kafkaVersionString

	// Print Sangrenel startup info.
	fmt.Printf("\nStarting %d client workers, %d writers per worker\n", Config.workers, Config.writersPerWorker)
	fmt.Printf("Message size %d bytes, %d message limit per batch\n", Config.msgSize, Config.batchSize)
	fmt.Printf("API Version: %s, Compression: %s, RequiredAcks: %s\n",
		version, Config.compressionName, Config.requiredAcksName)

	t := tachymeter.New(&tachymeter.Config{Size: 300000, Safe: true})

	// Start client workers.
	for i := 0; i < Config.workers; i++ {
		go worker(i+1, t)
	}

	var currSentCnt, lastSentCnt uint64
	var currErrCnt, lastErrCnt uint64

	interval := time.Duration(Config.interval) * time.Second
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
		fmt.Printf("> Messages: %s @ %.0f msgs/sec. | error rate %.2f%%\n",
			outputString,
			metrics["rate"],
			metrics["error_rate"])

		if !Config.noop {
			fmt.Printf("> Batches: %.2f batches/sec. | %s p99 | %s HMean | %s Min | %s Max\n",
				stats.Rate.Second, round(stats.Time.P99), round(stats.Time.HMean), round(stats.Time.Min), round(stats.Time.Max))

			fmt.Println(stats.Histogram.String(50))

			// Check if the tacymeter size needs to be increased to avoid
			// sampling. Otherwise, just reset it.
			if int(sentSinceLastInterval) > len(t.Times) {
				newTachy := tachymeter.New(&tachymeter.Config{Size: int(2 * sentSinceLastInterval)})
				// xxx this could swap in a tachy with unlocked mutexes
				// while the current one has locks held.
				*t = *newTachy
			} else {
				t.Reset()
			}
		}

		// Reset interval time.
		start = time.Now()
	}
}

// worker is a high level producer unit and holds a single Kafka client.
// The worker's Kafka client is shared by n (Config.writersPerWorker)
// writer instances that perform the message generation and writing.
func worker(n int, t *tachymeter.Tachymeter) {
	switch Config.noop {
	case false:
		cId := "worker_" + strconv.Itoa(n)

		conf := sarama.NewConfig()
		conf.Producer.Compression = Config.compression
		conf.Producer.Return.Successes = true
		conf.Producer.RequiredAcks = Config.requiredAcks
		conf.Net.MaxOpenRequests = Config.maxOpenRequests
		conf.Producer.Flush.MaxMessages = Config.batchSize
		conf.Producer.MaxMessageBytes = Config.msgSize + 50
		conf.Version = parseKafkaVersion(Config.kafkaVersionString)

		if Config.tls {

			tlsConfig := &tls.Config{
				InsecureSkipVerify: Config.tlsInsecureSkipVerify,
			}

			if len(Config.tlsCaCertificate) > 0 {
				caCert, err := ioutil.ReadFile(Config.tlsCaCertificate)
				if err != nil {
					log.Println(err)
					os.Exit(1)
				}
				caCertPool := x509.NewCertPool()
				caCertPool.AppendCertsFromPEM(caCert)
				tlsConfig.RootCAs = caCertPool
			}

			if len(Config.tlsCertificate) > 0 && len(Config.tlsPrivateKey) > 0 {
				cert, err := tls.LoadX509KeyPair(Config.tlsCertificate, Config.tlsPrivateKey)
				if err != nil {
					log.Fatal(err)
				}
				tlsConfig.Certificates = []tls.Certificate{cert}
			}

			conf.Net.TLS.Enable = true
			conf.Net.TLS.Config = tlsConfig
		}

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
	// If noop, we're not creating connections at all. Just generate
	// messages and burn CPU.
	default:
		for i := 0; i < Config.writersPerWorker; i++ {
			go dummyWriter(t)
		}
	}

	wait := make(chan bool)
	<-wait
}

// writer generates random messages and write to Kafka. Each writer
// belongs to a parent worker. Writers throttle writes according to a
// global rate limiter and report write throughput statistics up through
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
		var intervalSent uint64

		for {
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
				// of errors per message (if it returned an error).
				atomic.AddUint64(&errCnt, uint64(len(err.(sarama.ProducerErrors))))
				// Optionally report.
				if Config.emitErrors {
					for _, e := range err.(sarama.ProducerErrors) {
						fmt.Fprintf(os.Stderr, "%s\n", e.Error())
					}
				}
			}

			t.AddTime(time.Since(sendTime))
			atomic.AddUint64(&sentCnt, uint64(len(msgBatch)))

			msgBatch = msgBatch[:0]

			intervalSent = atomic.LoadUint64(&sentCnt) - countStart

			// Break if the global rate limit was met, or, if
			// we'd exceed it assuming all writers wrote a max batch size
			// for this interval.
			sendEstimate := intervalSent + uint64((Config.batchSize*Config.workers*Config.writersPerWorker)-Config.writersPerWorker)
			if sendEstimate >= Config.msgRate {
				break
			}
		}

		// If the global per-second rate limit was met, the inner loop
		// breaks and the outer loop sleeps for the interval remainder.
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

// calcOutput takes a duration t and messages sent and returns message
// rates in human readable network speeds.
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

func round(t time.Duration) time.Duration {
	return t / 1000 * 1000
}
