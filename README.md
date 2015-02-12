sangrenel
=========

"...basically a cloth bag filled with small jagged pieces of scrap iron"

NOTE: Sangrenel has a dependency on Shopify's Sarama Kafka client, which tends to change often. Subsequently, I have moved to managing this dependency as a local copy in the Sangrenel repo in accordance with the officially recommended Golang [guidance](http://golang.org/doc/faq#get_version). These changes are currently reflected in the Sangrenel 'dev' branch and are recommended if you'd prefer an easy time building Sangrenel. These changes will be merged into master pending some testing.

### Overview

Smashes Kafka queues with lots of messages. Usage overview:

<pre>
./sangrenel -h
Usage of ./sangrenel:
  -brokers="localhost:9092": Comma delimited list of Kafka brokers
  -noop=false: Test message generation performance, do not transmit messages
  -size=300: Message size in bytes
  -rate=100000000: Apply a global message rate limit
  -topic="sangrenel": Topic to publish to
  -workers=1: Number of Kafka client workers
</pre>

The <code>-workers</code> directive initializes n Kafka clients. Each client manages 5 Kafka producer instances in goroutines that synchronously publish random messages of <code>-size</code> bytes to the referenced Kafka cluster/topic as fast as possible. Kafka client instance (worker) counts need to be scaled up in order to produce more throughput, as each client connection maxes out throughput with roughly 5 producers instances. The <code>-rate</code> directive allows you to place a global cap on the total message rate for all workers combined.

Note: Sangrenel will automatically raise <code>GOMAXPROCS</code> to the value detected by <code>runtime.NumCPU()</code> to support increasing numbers of <code>-workers</code>. Sangrenel should be tested with <code>--noop</code> (messages are only generated but not transmitted to the brokers) in order to determine the maximum message rate that the configured message size and worker setting can generate. Otherwise, you may see a throughput rate that's computationally bound versus the actual limitation of the Kafka brokers being testing.

If a topic is referenced that does not yet exist, Sangrenel will create one with a default of 2 partitions / 1 replica (or as defined in your Kafka server configuration). Alternative parition/replica topologies should be created manually prior to running Sangrenel.

Sangrenel outputs metrics based on the previous 5 seconds of operation: the aggregate amount of data being produced, message transaction rate (or generated rate if using <code>--noop</code>) and 90th percentile worst latency average (time from message sent to receiving an ack from the broker). 

<pre>
$ ./sangrenel --size=2500 --workers=8 --topic=rep --brokers=10.0.1.37:9092,10.0.1.40:9092,10.0.1.62:9092

::: Sangrenel :::
Starting 8 workers
Message size 2500 bytes

client_7 connected
client_3 connected
client_8 connected
client_5 connected
client_6 connected
client_2 connected
client_1 connected
client_4 connected
2014-10-02T23:53:36Z Generating 546Mb/sec @ 28627 messages/sec | topic: rep | 3.32ms avg latency
2014-10-02T23:53:41Z Generating 528Mb/sec @ 27671 messages/sec | topic: rep | 3.55ms avg latency
2014-10-02T23:53:46Z Generating 516Mb/sec @ 27040 messages/sec | topic: rep | 3.73ms avg latency
2014-10-02T23:53:51Z Generating 478Mb/sec @ 25039 messages/sec | topic: rep | 4.65ms avg latency
</pre>

### Performance

Sangrenel obliterating all cores on an EC2 c3.8xlarge instance in <code>noop</code> mode (each case exceeding 3Gb/sec of raw, random data):

GOMAXPROCS=32 / 32 workers / 3500 byte message size: ~125,000 messages/sec

GOMAXPROCS=32 / 32 workers / 300 byte message size: ~1.38M messages/sec

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel.png)

### Docker

```
$ docker run x/sangrenel
Usage of /home/gocode/bin/sangrenel:
  -brokers="localhost:9092": Comma delimited list of Kafka brokers
  -noop=false: Test message generation performance, do not transmit messages
  -rate=100000000: Apply a global message rate limit
  -size=300: Message size in bytes
  -topic="sangrenel": Topic to publish to
  -workers=1: Number of Kafka client workers

$ docker run x/sangrenel -brokers="localhost:9092" -workers=8
...
```
