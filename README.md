# paxos

Description
===========
This is a simple implementation of the Paxos (http://en.wikipedia.org/wiki/Paxos_%28computer_science%29) consensus algorithm. This is meant as a playground, not for use in a production system at this stage.

Compilation
===========

Install godep executable and make sure it is in your path

```
go get github.com/tools/godep
```

then build

```
make
```

Running
=======
```
./paxos_server -httpports="8000,8001,8002,8003,8004" -rpcports="9000,9001,9002,9003,9004"
```

--httpports is a list of ports where nodes will listen for http requests that represent values that paxos will agree upon

--rpcports is a list of ports over which the nodes will talk to each other. There must be the same number of rpc ports as http ports.

Testing
=======

Now that paxos is up and running, let's give it some work to do:
```
for i in 1 2 3 4 5; do
    curl -d "1" localhost:8000/work & curl -d "2" localhost:8001/work & curl -d "3" localhost:8002/work &
done
```

Valid work consists of integers greater than 0. As you perform requests, the paxos server will print out the numbers you submitted in an agreed order among the nodes in the following format:

```
[port]Item: number
[8000]Item: 1
[8001]Item: 1
[8002]Item: 1
[8003]Item: 1
[8004]Item: 1
[8000]Item: 2
[8000]Item: 3
[8001]Item: 2
[8001]Item: 3
[8002]Item: 2
[8002]Item: 3
[8003]Item: 2
[8003]Item: 3
[8004]Item: 2
[8004]Item: 3
[8000]Item: 1
```

The paxos algorithm really comes to light when you look at the orders of the individual ports, they should be the same

8000: 1 2 3 1 3 2 1 2 3 1 3 2 1 2 3 1 3 2 1 2 3 1 3

8001: 1 2 3 1 3 2 1 2 3 1 3 2 1 2 3 1 3 2 1 2 3 1 3



