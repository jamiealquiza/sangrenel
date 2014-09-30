sangrenel
=========

"...basically a cloth bag filled with small jagged pieces of scrap iron"

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

The <code>workers</code> directive initializes n Kafka clients. Each client manages 5 Kafka producer instances in goroutines that synchronously publish random messages of <code>-size</code> bytes to the referenced Kafka cluster/topic as fast as possible. Kafka client instance counts need to be scaled up in order to produce more throughput, as each client connection maxes out throughput with roughly 5 producers. 

Note: You should also raise the <code>GOMAXPROCS</code> environment variable to support increasing numbers of clients. Sangrenel should be tested with <code>--noop=true</code> (messages are only generated but not transmitted to the brokers) in order to determine the minimum message rate that the configured message size and worker concurrency can generate. Otherwise, you may see a throughput rate that's computationally bound versus the actual limitation of the Kafka brokers that you're testing.

If a topic is referenced that does not yet exist, Sangrenel will create one with a default of 2 partitions / 1 replica (or as defined in your Kafka server configuration). Alternative parition/replica topologies should be created manually prior to running Sangrenel.

<pre>
$ ./sangrenel --workers=10 --size=800 --topic=test --brokers=10.0.1.37:9092,10.0.1.40:9092,10.0.1.62:9092

::: Sangrenel :::
Starting 10 workers
Message size 800 bytes

client_2 connected
client_3 connected
client_9 connected
client_7 connected
client_10 connected
client_4 connected
client_6 connected
client_8 connected
client_1 connected
client_5 connected
27649 messages/sec published to topic: test
28478 messages/sec published to topic: test
28037 messages/sec published to topic: test
</pre>
