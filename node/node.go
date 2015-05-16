package node

import (
	"paxos/paxos"
	"sync"
	"time"
)

type WorkItem int

type Node struct {
	workQueue             []WorkItem
	workQueueLock         *sync.Mutex
	proposalNumberMapLock *sync.Mutex
	paxos                 *paxos.Paxos

	// This will have the results
	ProposalNumberToValueMap map[uint64]uint64
	CommitNumber             uint64
}

func NewNode(tcpPort string, clientAddresses []string) *Node {
	node := &Node{
		workQueue:             make([]WorkItem, 0, 1000),
		workQueueLock:         &sync.Mutex{},
		proposalNumberMapLock: &sync.Mutex{},
	}
	node.paxos = &paxos.Paxos{
		ListenPort:      tcpPort,
		ClientAddresses: clientAddresses,
		CommitCallback:  node.AppendWorkItemResult,
		GetValueCallback: func(proposalNumber uint64) uint64 {
			node.workQueueLock.Lock()
			defer node.workQueueLock.Unlock()
			if val, ok := node.ProposalNumberToValueMap[proposalNumber]; ok {
				return val
			}
			return 0
		},
	}
	return node
}

// Start the paxos algorithm
func (self *Node) InitPaxos() {
	self.CommitNumber = 1
	go self.paxos.Run()
}

// Accept work into a queue3
func (self *Node) AppendWork(workItem WorkItem) {
	self.workQueueLock.Lock()
	defer self.workQueueLock.Unlock()

	// append item to the queue
	self.workQueue = append(self.workQueue, workItem)
}

func (self *Node) AppendWorkItemResult(proposalNumber uint64, workItem uint64) {
	self.proposalNumberMapLock.Lock()
	defer self.proposalNumberMapLock.Unlock()
	if _, ok := self.ProposalNumberToValueMap[proposalNumber]; ok {
		panic("We already have a value for this propsal number! something went terribly wrong")
	}
	self.ProposalNumberToValueMap[proposalNumber] = workItem
}

// Process work after reaching consensus
func (self *Node) ProcessWork(workPerformed chan WorkItem) {
	self.ProposalNumberToValueMap = make(map[uint64]uint64)

	// Commit items that are ready periodically
	go func() {
		for {
			self.proposalNumberMapLock.Lock()
			if _, ok := self.ProposalNumberToValueMap[self.CommitNumber]; ok {
				for {
					workPerformed <- WorkItem(self.ProposalNumberToValueMap[self.CommitNumber])
					self.CommitNumber++
					if _, ok := self.ProposalNumberToValueMap[self.CommitNumber]; !ok {
						break
					}
				}
			}
			self.proposalNumberMapLock.Unlock()
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// This loop will run the paxos algorithm and process work items
	// in such a way that everybody agrees on the order in which they
	// are performed
	for {

		self.workQueueLock.Lock()
		if len(self.workQueue) == 0 {
			self.workQueueLock.Unlock()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		workItem := self.workQueue[0]
		self.workQueueLock.Unlock()

		// propose the first item in our queue to paxos!
		// Paxos will not give up on us until this is completed,
		// so we can safely delete from our queue
		go self.paxos.Propose(uint64(workItem))

		// perform work & delete chosen item from the queue
		self.workQueueLock.Lock()
		for i, item := range self.workQueue {
			if item == workItem {
				if i == 0 {
					self.workQueue = self.workQueue[:0]
				} else {
					copy(self.workQueue[i:], self.workQueue[i+1:])
					self.workQueue = self.workQueue[:len(self.workQueue)-1]
				}
				break
			}
		}
		self.workQueueLock.Unlock()
	}
}
