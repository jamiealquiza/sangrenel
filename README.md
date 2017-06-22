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
        Top-level Graphite namespace prefix (defaults to hostname) (default "ja.local")
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
  -required-acks string
        RequiredAcks config: none, local, all (default "local")
  -topic string
        Kafka topic to produce to (default "sangrenel")
  -workers int
        Number of workers (default 1)
  -writers-per-worker int
        Number of writer (Kafka producer) goroutines per worker (default 5)
</pre>

Sangrenel uses the Kafka client library, [Sarama](https://github.com/Shopify/sarama). Sangrenel starts one or more workers, each of which maintain a unique Kafka client connection to the target cluster. Each worker has a number of writers which generate and send message data to Kafka, sharing the parent worker client connection. The number of workers is configurable via the `-workers` flag, the number of writers per worker via the `-writers-per-worker`. This is done for scaling purposes; while a single Sarama client can be used for multiple writers (which live in separate goroutines), performance begings to flatline at a certain point. It's best to leave the writers-per-worker at the default 5 and scaling the worker count as needed, but the option is exposed for more control. Left as a technical exercise for the user, there's a different between 2 workers with 5 writers each and 1 worker with 10 writers.

The `-topic` flag specifies which topic is used, allowing configs such as parition count and replication factor to be prepared ahead of time for performance comparisons (by switching which topic Sangrenel is using). The `-message-batch-size`, `-message-size` and `-produce-rate` flags can be used to dictate message size, number of messages to batch per write, and the total Sangrenel write rate.  `-required-acks` sets the Sarama [RequiredAcks](https://godoc.org/github.com/Shopify/sarama#RequiredAcks) config.

Two important factors to note:
- Sangrenel uses Sarama's [SyncProducer](https://godoc.org/github.com/Shopify/sarama#SyncProducer), meaning messages are written synchronously
- At a given message size, Sangrenel should be tested in `-noop` mode to ensure the desired number of messages can be generated (even if a `-produce-rate` is specified)

Once running, Sangrenel generates and writes messages as fast as possible (or to the configured `-produce-rate`). Every 5 seconds, message throughput, rates, latency and other metrics (via [tachymeter](https://github.com/jamiealquiza/tachymeter)) are printed to console.

If optionally defined, some metric data can be written to Graphite. More/better metric output options will be added.

### Example

<pre>
% sangrenel -message-batch-size=50 -message-size=180 -produce-rate=5000

Starting 1 client workers, 5 writers per worker
Message size 180 bytes, 50 message limit per batch
Compression: none, RequiredAcks: local
2017/06/22 12:46:43 worker_1 connected

2017/06/22 12:46:48 [ topic: sangrenel ]
> Producing 7Mb/sec @ 4995 msgs/sec. | 4.46ms p99 batch write | error rate 0.00%
> Batch Statistics, Last 5.0s:
500 samples of 500 events
Cumulative:     825.042897ms
HMean:          1.453488ms
Avg.:           1.650085ms
p50:            1.356251ms
p75:            1.715414ms
p95:            3.228459ms
p99:            4.455199ms
p999:           8.775584ms
Long 5%:        4.529264ms
Short 5%:       1.024227ms
Max:            8.775584ms
Min:            924.004µs
Range:          7.85158ms
Rate/sec.:      99.91

2017/06/22 12:46:53 [ topic: sangrenel ]
> Producing 7Mb/sec @ 5000 msgs/sec. | 2.98ms p99 batch write | error rate 0.00%
> Batch Statistics, Last 5.0s:
500 samples of 500 events
Cumulative:     674.712093ms
HMean:          1.293377ms
Avg.:           1.349424ms
p50:            1.252731ms
p75:            1.359836ms
p95:            2.038691ms
p99:            2.978776ms
p999:           3.539097ms
Long 5%:        2.668488ms
Short 5%:       994.556µs
Max:            3.539097ms
Min:            770.541µs
Range:          2.768556ms
Rate/sec.:      100.01
</pre>

### Misc.

Messages/sec. vs latency output, Graphite output:

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-graphite0.png)

MB/s. vs latency (Sangrenel writes byte values; this can also be viewed as Mb and Gb in Grafana):

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-graphite1.png)


Sangrenel in `noop` mode generating over 6.4Gb/s of random message data on a c4.8xlarge:

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-c4.png)
