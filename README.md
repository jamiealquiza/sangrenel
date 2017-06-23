sangrenel
=========

[Update] Sangrenel is currently being updated. Take note of [issues](https://github.com/jamiealquiza/sangrenel/issues).

*"...basically a cloth bag filled with small jagged pieces of scrap iron"*

Sangrenel is Kafka cluster load testing tool. Sangrenel was originally created for some baseline performance testing, exemplified in my *Load testing Apache Kafka on AWS* [blog post](https://grey-boundary.io/load-testing-apache-kafka-on-aws/).

While using this tool, keep in mind that benchmarking is always an examination of total systems performance, and that the testing software itself is part of the system (meaning you're not just testing Kafka, but Kafka+Sangrenel).

### Example

Sangrenel takes [configurable](https://github.com/jamiealquiza/sangrenel#usage) message/batch sizing, concurrency and other settings and writes messages to a reference topic. Write latency, distribution, and throughput data is dumped every 5 seconds. 

<pre>
% sangrenel -message-size=800 -message-batch-size=500

Starting 1 client workers, 5 writers per worker
Message size 800 bytes, 500 message limit per batch
Compression: none, RequiredAcks: local
2017/06/23 11:35:21 worker_1 connected

2017/06/23 11:35:26 [ topic: sangrenel ]
> Messages: 967Mb/sec @ 158377 msgs/sec. | error rate 0.00%
> Batches: 316.75 batches/sec. | 8.178ms p99 | 3.962ms HMean | 2.481ms Min | 18.032ms Max
   2.481ms - 4.036ms -------------------------
   4.036ms - 5.591ms ----------------
   5.591ms - 7.146ms ----
   7.146ms - 8.701ms -
  8.701ms - 10.256ms -
 10.256ms - 11.811ms -
 11.811ms - 13.367ms -
 13.367ms - 14.922ms -
 14.922ms - 16.477ms -
 16.477ms - 18.032ms -


2017/06/23 11:35:31 [ topic: sangrenel ]
> Messages: 992Mb/sec @ 162449 msgs/sec. | error rate 0.00%
> Batches: 324.90 batches/sec. | 8.084ms p99 | 3.961ms HMean | 2.519ms Min | 10.52ms Max
   2.519ms - 3.319ms -------------
   3.319ms - 4.119ms -------------------------
   4.119ms - 4.919ms ----------------
   4.919ms - 5.719ms -------
   5.719ms - 6.519ms ---
   6.519ms - 7.319ms --
   7.319ms - 8.119ms -
   8.119ms - 8.919ms -
   8.919ms - 9.719ms -
   9.719ms - 10.52ms -


2017/06/23 11:35:36 [ topic: sangrenel ]
> Messages: 906Mb/sec @ 148428 msgs/sec. | error rate 0.00%
> Batches: 296.86 batches/sec. | 11.51ms p99 | 4.37ms HMean | 2.633ms Min | 13.922ms Max
   2.633ms - 3.762ms -------------
   3.762ms - 4.891ms -------------------------
    4.891ms - 6.02ms -----------
    6.02ms - 7.148ms ---
   7.148ms - 8.277ms -
   8.277ms - 9.406ms -
  9.406ms - 10.535ms -
 10.535ms - 11.664ms -
 11.664ms - 12.793ms -
 12.793ms - 13.922ms -
</pre>

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

If optionally defined, some metric data can be written to Graphite. Better metric output options will be added soon.
