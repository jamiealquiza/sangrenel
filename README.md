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
- At a given message size, Sangrenel should be tested in `-noop` mode to ensure the desired number of messages can be generated (even if a `-produce-rate` is specified)

Once running, Sangrenel generates and writes messages as fast as possible (or to the configured `-produce-rate`). Every 5 seconds, message throughput, rates, latency and other metrics (via [tachymeter](https://github.com/jamiealquiza/tachymeter)) are printed to console.

If optionally defined, some metric data can be written to Graphite. More/better metric output options will be added.

### Example

<pre>
% sangrenel -message-batch-size=250 -message-size=180 -produce-rate=5000
                                 
Starting 1 client workers, 5 writers per worker
Message size 180 bytes, 250 message limit per batch
Compression: none                
2017/06/22 11:32:41 worker_1 connected
                                 
2017/06/22 11:32:46 Generating 7Mb/sec @ 4996 messages/sec | topic: sangrenel | 15.64ms p99 batch latency
> Batch Statistics, Last 5.0s:   
100 samples of 100 events
Cumulative:     467.289815ms
HMean:          3.815624ms
Avg.:           4.672898ms
p50:            3.923364ms
p75:            5.242536ms
p95:            10.149782ms
p99:            15.644189ms
p999:           15.834959ms
Long 5%:        12.55286ms
Short 5%:       2.085274ms
Max:            15.834959ms
Min:            1.794895ms
Range:          14.040064ms
Rate/sec.:      19.98

2017/06/22 11:32:51 Generating 7Mb/sec @ 5003 messages/sec | topic: sangrenel | 5.74ms p99 batch latency
> Batch Statistics, Last 5.0s:
100 samples of 100 events
Cumulative:     342.375048ms
HMean:          3.172272ms
Avg.:           3.42375ms
p50:            3.3304ms
p75:            3.977578ms
p95:            4.974324ms
p99:            5.741539ms
p999:           6.122486ms
Long 5%:        5.427207ms
Short 5%:       1.797883ms
Max:            6.122486ms
Min:            1.678467ms
Range:          4.444019ms
Rate/sec.:      20.01
</pre>

### Misc.

Messages/sec. vs latency output, Graphite output:

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-graphite0.png)

MB/s. vs latency (Sangrenel writes byte values; this can also be viewed as Mb and Gb in Grafana):

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-graphite1.png)


Sangrenel in `noop` mode generating over 6.4Gb/s of random message data on a c4.8xlarge:

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-c4.png)
