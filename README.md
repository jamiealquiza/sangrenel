sangrenel
=========

"...basically a cloth bag filled with small jagged pieces of scrap iron"

### Installation
NOTE: Sangrenel has a dependency on Shopify's Sarama Kafka client, which tends to change often. Subsequently, I have moved to managing this dependency as a local copy in the Sangrenel repo in accordance with the officially recommended Golang [guidance](http://golang.org/doc/faq#get_version).

Sarama is currently vendored at Sarama v1.0.0.

Assuming Go is installed (tested up to version 1.4.1) and $GOPATH is set:

- `go get github.com/jamiealquiza/sangrenel`
- `go build github.com/jamiealquiza/sangrenel`

Binary will be found at `$GOPATH/bin/sangrenel`

### Overview

Smashes Kafka queues with lots of messages. Usage overview:

<pre>
./sangrenel -h
Usage of ./sangrenel:
  -brokers="localhost:9092": Comma delimited list of Kafka brokers
  -clients=1: Number of Kafka client workers
  -noop=false: Test message generation performance, do not transmit messages
  -producers=5: Number of producer instances per client
  -rate=100000000: Apply a global message rate limit
  -size=300: Message size in bytes
  -topic="sangrenel": Topic to publish to
</pre>

The <code>-clients</code> directive initializes n Kafka clients. Each client manages 5 Kafka producer instances (overridden with <code>-producers</code>) in goroutines that synchronously publish random messages of <code>-size</code> bytes to the referenced Kafka cluster/topic, as fast as possible. Kafka client worker counts need to be scaled up in order to produce more throughput, as each client connection maxes out throughput with roughly 5 producers instances. Configuring these variables allows you to roughly model arbitary topologies of connection counts and workers per connection. The <code>-rate</code> directive allows you to place a global cap on the total message rate for all workers combined.

Note: Sangrenel will automatically raise <code>GOMAXPROCS</code> to the value detected by <code>runtime.NumCPU()</code> to support increasing numbers of <code>-clients</code>. Sangrenel should be tested with <code>--noop</code> (messages are only generated but not transmitted to the brokers) in order to determine the maximum message rate that the configured message size and worker setting can generate. Otherwise, you may see a throughput rate that's computationally bound versus the actual limitation of the Kafka brokers being testing.

If a topic is referenced that does not yet exist, Sangrenel will create one with a default of 2 partitions / 1 replica (or as defined in your Kafka server configuration). Alternative parition/replica topologies should be created manually prior to running Sangrenel.

Sangrenel outputs metrics based on the previous 5 seconds of operation: the aggregate amount of data being produced, message transaction rate (or generated rate if using <code>--noop</code>) and 90th percentile worst latency average (time from message sent to receiving an ack from the broker). 

<pre>
% ./sangrenel -brokers="192.168.100.204:9092" -size=250 -topic=load -clients=4

::: Sangrenel :::
Starting 4 client workers, 5 producers per worker
Message size 250 bytes

2015/03/18 13:41:22 client_1 connected
2015/03/18 13:41:22 client_3 connected
2015/03/18 13:41:22 client_4 connected
2015/03/18 13:41:22 client_2 connected
2015/03/18 13:41:27 Generating 29Mb/sec @ 15224 messages/sec | topic: load | 2.08ms 90%ile latency
2015/03/18 13:41:32 Generating 29Mb/sec @ 15234 messages/sec | topic: load | 2.07ms 90%ile latency
2015/03/18 13:41:37 Generating 29Mb/sec @ 15324 messages/sec | topic: load | 2.10ms 90%ile latency
2015/03/18 13:41:42 Generating 30Mb/sec @ 15574 messages/sec | topic: load | 2.01ms 90%ile latency
</pre>

### Performance

Sangrenel obliterating all cores on an EC2 c4.8xlarge instance in <code>noop</code> mode, generating over 6.4Gb/s of random message data:

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel-c4.png)
