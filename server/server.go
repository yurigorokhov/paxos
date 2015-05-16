package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"paxos/node"
	"strconv"
	"strings"
)

type NodeServer struct {
	Node *node.Node
}

// accept work and pass it on to the node
func (self *NodeServer) performWork(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}
	i, err := strconv.Atoi(string(body))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}
	self.Node.AppendWork(node.WorkItem(i))
	w.WriteHeader(http.StatusOK)
}

func (self *NodeServer) Run(rpcPorts []string, rpcPortIdx int, httpPort string) {
	srvMux := http.NewServeMux()
	srvMux.HandleFunc("/work", self.performWork)

	// Work performed channel
	workChan := make(chan node.WorkItem, 1000)

	// convert rpc ports to addresses
	addresses := make([]string, 0, len(rpcPorts))
	for _, port := range rpcPorts {
		addresses = append(addresses, "localhost:"+port)
	}
	self.Node = node.NewNode(rpcPorts[rpcPortIdx], addresses)
	self.Node.InitPaxos()
	go self.Node.ProcessWork(workChan)
	go func() {
		for {
			item := <-workChan
			fmt.Printf("\n[%s]Item: %v", httpPort, item)
		}
	}()

	// listen for incoming work
	http.ListenAndServe(fmt.Sprintf(":%s", httpPort), srvMux)
}

func main() {

	// read config file and launch a bunch of servers on different ports
	var httpPortString, rpcPortString string
	flag.StringVar(&httpPortString, "httpports", "8000", "comma separated list of ports")
	flag.StringVar(&rpcPortString, "rpcports", "9000", "comma separated list of ports, must be same number as ports")
	flag.Parse()
	httpPorts := strings.Split(httpPortString, ",")
	rpcPorts := strings.Split(rpcPortString, ",")
	if len(httpPorts) != len(rpcPorts) {
		fmt.Errorf("ports and rpcports must be equal in number")
	}
	for i, httpPort := range httpPorts {
		s := &NodeServer{}
		go s.Run(rpcPorts, i, httpPort)
	}
	var input string
	for {
		_, _ = fmt.Scanf("%s", &input)
		if input == "x" {
			break
		}
	}
}
