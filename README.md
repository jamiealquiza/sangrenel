sangrenel
=========

"...basically a cloth bag filled with small jagged pieces of scrap iron"

Smashes Kafka queues with lots of messages.

<pre>
$ ./sangrenel --workers=10 --topic=test --brokers=10.0.1.37:9092,10.0.1.40:9092,10.0.1.62:9092

::: Sangrenel :::
Starting 10 workers

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
