sangrenel
=========

[Update] Sangrenel is currently being updated.

*"...basically a cloth bag filled with small jagged pieces of scrap iron"*

Sangrenel is Kafka cluster load testing tool. Sangrenel was originally created for some baseline performance testing, exemplified in my *Load testing Apache Kafka on AWS* [blog post](https://grey-boundary.io/load-testing-apache-kafka-on-aws/).

While using this tool, keep in mind that benchmarking is always an examination of total systems performance, and that the testing software itself is part of the system (meaning you're not just testing Kafka, but Kafka+Sangrenel).

### Installation

Assuming Go is installed (tested with 1.7+) and $GOPATH is set:

- `go get -u github.com/jamiealquiza/sangrenel`
- `go install github.com/jamiealquiza/sangrenel`

Binary will be found at `$GOPATH/bin/sangrenel`

### Usage

Usage output:
<pre>
Usage of sangrenel:
  -brokers string
        Comma delimited list of Kafka brokers (default "localhost:9092")
  -compression string
        Message compression: none, gzip, snappy (default "none")
  -graphite-ip string
        Destination Graphite IP address
  -graphite-metrics-prefix string
        Top-level Graphite namespace prefix (defaults to hostname) (default "mbp.local")
  -graphite-port string
        Destination Graphite plaintext port
  -message-batch-size int
        Messages per batch (default 1)
  -message-size int
        Message size (bytes) (default 300)
  -noop
        Test message generation performance (does not connect to Kafka)
  -produce-rate uint
        Global write rate limit (messages/sec) (default 100000000)
  -topic string
        Kafka topic to produce to (default "sangrenel")
  -workers int
        Number of workers (default 1)
  -writers-per-worker int
        Number of writer (Kafka producer) goroutines per worker (default 5)
</pre>

Sangrenel uses the Kafka client library, [Sarama](https://github.com/Shopify/sarama). Sangrenel starts one or more workers, each of which maintain a unique Kafka client connection to the target cluster. Each worker has a number of writers which generate and send message data to Kafka, sharing the parent worker client connection. The number of workers is configurable via the `-workers` flag, the number of writers per worker via the `-writers-per-worker`. This is done for scaling purposes; while a single Sarama client can be used for multiple writers (which live in separate goroutines), performance begings to flatline at a certain point. It's best to leave the writers-per-worker at the default 5 and scaling the worker count as needed, but the option is exposed for more control. Left as a technical exercise for the user, there's a different between 2 workers with 5 writers each and 1 worker with 10 writers.

The `-topic` flag specifies which topic is used, allowing configs such as parition count and replication factor to be prepared ahead of time for performance comparisons (by switching which topic Sangrenel is using). The `-message-batch-size`, `-message-size` and `-produce-rate` flags can be used to dictate message size, number of messages to batch per write, and the total Sangrenel write rate. 

Two important factors to note:
- Sangrenel uses Sarama's [SyncProducer](https://godoc.org/github.com/Shopify/sarama#SyncProducer), meaning messages are written synchronously
- At a given message size, run Sangrenel in `-noop` mode to ensure the desired number of messages can be generated (even if a `-produce-rate` is specified)

Once running, Sangrenel generates and writes messages as fast as possible (or to the configured `-produce-rate`). Every 5 seconds, message throughput rates, latency and other metrics (via [tachymeter](https://github.com/jamiealquiza/tachymeter)) are printed to console.

If optionally defined, some metric data can be written to Graphite. More/better metric output options will be added.

### Example

<pre>
% sangrenel -brokers="localhost:9092" -message-size=250 -topic=test -workers=3

Starting 3 client workers, 5 writers per worker
Message size 250 bytes, 1 message limit per batch
Compression: none
2017/06/21 17:32:06 worker_2 connected
2017/06/21 17:32:06 worker_3 connected
2017/06/21 17:32:06 worker_1 connected

2017/06/21 17:32:11 Generating 50Mb/sec @ 26446 messages/sec | topic: test | 0.84ms p99 latency
132279 samples of 132279 events
Cumulative:     1m13.628081218s
HMean:          546.495µs
Avg.:           556.612µs
p50:            541.849µs
p75:            582.889µs
p95:            675.962µs
p99:            838.907µs
p999:           1.155118ms
Long 5%:        808.479µs
Short 5%:       445.683µs
Max:            7.052061ms
Min:            173.118µs
Range:          6.878943ms
Rate/sec.:      26446.33

2017/06/21 17:32:16 Generating 51Mb/sec @ 26494 messages/sec | topic: test | 0.84ms p99 latency
131189 samples of 131189 events
Cumulative:     1m12.974667212s
HMean:          546.536µs
Avg.:           556.255µs
p50:            539.445µs
p75:            583.587µs
p95:            690.266µs
p99:            844.876µs
p999:           1.132036ms
Long 5%:        800.887µs
Short 5%:       448.191µs
Max:            3.887683ms
Min:            194.041µs
Range:          3.693642ms
Rate/sec.:      26493.92

2017/06/21 17:32:21 Generating 51Mb/sec @ 26632 messages/sec | topic: test | 0.82ms p99 latency
131973 samples of 131973 events
Cumulative:     1m12.986170902s
HMean:          544.872µs
Avg.:           553.038µs
p50:            541.944µs
p75:            578.615µs
p95:            653.687µs
p99:            818.307µs
p999:           1.134189ms
Long 5%:        770.477µs
Short 5%:       451.758µs
Max:            3.928126ms
Min:            210.161µs
Range:          3.717965ms
Rate/sec.:      26631.91
</pre>

### Misc.

Messages/sec. vs latency output, Graphite output:

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-graphite0.png)

MB/s. vs latency (Sangrenel writes byte values; this can also be viewed as Mb and Gb in Grafana):

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-graphite1.png)


Sangrenel in `noop` mode generating over 6.4Gb/s of random message data on a c4.8xlarge:

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-c4.png)