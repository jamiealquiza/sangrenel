sangrenel
=========

*"...basically a cloth bag filled with small jagged pieces of scrap iron"*

### Installation

NOTE: Sangrenel locally vendors v1.8.0 of Sarama (the Go Kafka client library), which builds properly with Sangrenel - but the functionally with this version has not been extensively tested.

Assuming Go is installed (tested with 1.6) and $GOPATH is set:

- `go get github.com/jamiealquiza/sangrenel`
- `go install github.com/jamiealquiza/sangrenel`

Binary will be found at `$GOPATH/bin/sangrenel`

### Overview

Smashes Kafka queues with lots of messages and reports performance metrics (to console and optionally, Graphite). Used in my Kafka on AWS load testing [blog post](https://grey-boundary.io/load-testing-apache-kafka-on-aws/). While using this tool, keep in mind that any benchmarking scenario is an examination of total systems performance and that the testing software itself is part of the system (meaning you're not just testing Kafka, but Kafka+Sangrenel).

Usage overview:

<pre>
% ./sangrenel -h
Usage of ./sangrenel:
  -batch=0: Max messages per batch. Defaults to unlimited (0).
  -brokers="localhost:9092": Comma delimited list of Kafka brokers
  -clients=1: Number of Kafka client workers
  -compression="none": Message compression: none, gzip, snappy
  -graphite-ip="": Destination Graphite IP address
  -graphite-metrics-prefix="random": Top-level Graphite namespace prefix (defaults to hostname)
  -graphite-port="": Destination Graphite plaintext port
  -noop=false: Test message generation performance, do not transmit messages
  -producers=5: Number of producer instances per client
  -rate=100000000: Apply a global message rate limit
  -size=300: Message size in bytes
  -topic="sangrenel": Topic to publish to
</pre>

The <code>-clients</code> directive initializes n Kafka clients. Each client manages 5 Kafka producer instances (overridden with <code>-producers</code>) in goroutines that synchronously publish random messages of <code>-size</code> bytes to the referenced Kafka cluster/topic as fast as possible. Kafka client worker counts need to be scaled up in order to produce more throughput, as each client connection maxes out throughput with roughly 5 producers instances. Configuring these variables allows you to roughly model arbitary topologies of connection counts and workers per connection. The <code>-rate</code> directive allows you to place a global cap on the total message rate for all workers combined.

Note: Sangrenel should be tested with <code>--noop</code> (messages are only generated but not transmitted to the brokers) in order to determine the maximum message rate that the configured message size and worker setting can generate. Otherwise, you may see a throughput rate that's computationally bound versus the actual limitation of the Kafka brokers being testing.

If a topic is referenced that does not yet exist, Sangrenel will create one with a default of 2 partitions / 1 replica (or as defined in your Kafka server configuration). Alternative parition/replica topologies should be created manually prior to running Sangrenel.

Sangrenel outputs metrics based on the previous 5 seconds of operation: the aggregate amount of data being produced, message transaction rate (or generated rate if using <code>--noop</code>) and top 10% worst latency average (time from message sent to receiving an ack from the broker).

If optionally defined, Graphite can be used as a secondary output location. This allows you to graph performance results in addition to overlaying Sangrenel metrics against Kafka cluster metrics that you may already be collecting in Graphite.

<pre>
% ./sangrenel -brokers="192.168.100.204:9092" -size=250 -topic=load -clients=3 

::: Sangrenel :::

Starting 3 client workers, 5 producers per worker
Message size 300 bytes, 0 message limit per batch
Compression: none
2016/10/10 17:27:16 client_2 connected
2016/10/10 17:27:16 client_1 connected
2016/10/10 17:27:16 client_3 connected

2016/10/10 17:27:21 Generating 84Mb/sec @ 36590 messages/sec | topic: sangrenel | 0.66ms top 10% latency
182950 samples of 182950 events
Total:			1m12.664330146s
Avg.:			397.181µs
Median: 		381.957µs
95%ile:			490.055µs
Longest 5%:		656.682µs
Shortest 5%:	319.556µs
Max:			24.062776ms
Min:			130.88µs
Rate/sec.:		36589.96

2016/10/10 17:27:26 Generating 82Mb/sec @ 35969 messages/sec | topic: sangrenel | 0.64ms top 10% latency
178730 samples of 178730 events
Total:			1m12.351815251s
Avg.:			404.81µs
Median: 		388.343µs
95%ile:			530.174µs
Longest 5%:		642.02µs
Shortest 5%:	312.688µs
Max:			3.790078ms
Min:			93.938µs
Rate/sec.:		35968.50

2016/10/10 17:27:31 Generating 79Mb/sec @ 34809 messages/sec | topic: sangrenel | 0.74ms top 10% latency
172750 samples of 172750 events
Total:			1m12.315903042s
Avg.:			418.615µs
Median: 		394.741µs
95%ile:			574.729µs
Longest 5%:		740.951µs
Shortest 5%:	316.336µs
Max:			4.230673ms
Min:			118.54µs
Rate/sec.:		34809.11
</pre>

Messages/sec. vs latency output, Graphite output:

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-graphite0.png)

MB/s. vs latency (Sangrenel writes byte values; this can also be viewed as Mb and Gb in Grafana):

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-graphite1.png)

### Performance

Sangrenel obliterating all cores on an EC2 c4.8xlarge instance in <code>noop</code> mode, generating over 6.4Gb/s of random message data:

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-c4.png)
