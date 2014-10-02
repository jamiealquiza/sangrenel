sangrenel
=========

"...basically a cloth bag filled with small jagged pieces of scrap iron"

### Overview

Smashes Kafka queues with lots of messages. Sangrenel accepts the following flags:

<pre>
./sangrenel -h
Usage of ./sangrenel:
  -brokers="localhost:9092": Comma delimited list of Kafka brokers
  -noop=false: Test message generation performance, do not transmit messages
  -size=300: Message size in bytes
  -topic="sangrenel": Topic to publish to
  -workers=1: Number of Kafka client workers
</pre>

The <code>-workers</code> directive initializes n Kafka clients. Each client manages 5 Kafka producer instances in goroutines that synchronously publish random messages of <code>-size</code> bytes to the referenced Kafka cluster/topic as fast as possible. Kafka client instance counts need to be scaled up in order to produce more throughput, as each client connection maxes out throughput with roughly 5 producers instances. 

Note: Sangrenel will automatically raise <code>GOMAXPROCS</code> to the value detected by <code>runtime.NumCPU()</code> to support increasing numbers of <code>-workers</code>. Sangrenel should be tested with <code>--noop=true</code> (messages are only generated but not transmitted to the brokers) in order to determine the minimum message rate that the configured message size and worker concurrency can generate. Otherwise, you may see a throughput rate that's computationally bound versus the actual limitation of the Kafka brokers being testing.

If a topic is referenced that does not yet exist, Sangrenel will create one with a default of 2 partitions / 1 replica (or as defined in your Kafka server configuration). Alternative parition/replica topologies should be created manually prior to running Sangrenel.

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
Producing 533Mb/sec raw data @ 27938 messages/sec - topic: rep
Producing 539Mb/sec raw data @ 28236 messages/sec - topic: rep
Producing 533Mb/sec raw data @ 27940 messages/sec - topic: rep
Producing 485Mb/sec raw data @ 25452 messages/sec - topic: rep
Producing 530Mb/sec raw data @ 27798 messages/sec - topic: rep
</pre>

### Performance

Sangrenel obliterating all cores on an EC2 c3.8xlarge instance in <code>noop</code> mode (each case exceeding 3Gb/sec of raw, random data):

GOMAXPROCS=32 / 32 workers / 3500 byte message size: ~125,000 messages/sec

GOMAXPROCS=32 / 32 workers / 300 byte message size: ~1.38M messages/sec

![ScreenShot](http://us-east.manta.joyent.com/jalquiza/public/github/sangrenel.png)
