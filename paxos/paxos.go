package paxos

import (
	"fmt"
	"log"
	"net"
	pr "paxos/paxosrpc"
	"sync"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type Paxos struct {
	ListenPort       string
	ClientAddresses  []string
	CommitCallback   func(commitNumber uint64, workItem uint64)
	GetValueCallback func(commitNumber uint64) uint64

	// Paxos state
	HighestProposalNumber            uint64
	PreviouslyAcceptedProposalNumber uint64
	PreviouslyAcceptedValue          uint64
	PaxosDone                        bool
	paxosStateLock                   *sync.Mutex
}

// Runs the RPC server for this paxos node
func (self *Paxos) Run() {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", self.ListenPort))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	self.PaxosDone = true
	self.paxosStateLock = &sync.Mutex{}
	grpcServer := grpc.NewServer()
	pr.RegisterPaxosRoutesServer(grpcServer, self)
	grpcServer.Serve(lis)
}

// Handles when a value is proposed
func (self *Paxos) ProposeValue(ctx context.Context, in *pr.ProposeRequest) (*pr.ProposeReply, error) {
	self.paxosStateLock.Lock()
	defer self.paxosStateLock.Unlock()
	self.PaxosDone = false
	if in.ProposalNumber <= self.HighestProposalNumber {

		// we have already seen a proposal of a higher value, send back the value at that number
		return &pr.ProposeReply{HighestProposalNumber: self.HighestProposalNumber, Success: false, MissingValue: self.GetValueCallback(in.ProposalNumber)}, nil
	}
	prevHighestProposal := self.HighestProposalNumber
	self.HighestProposalNumber = in.ProposalNumber
	return &pr.ProposeReply{
		HighestProposalNumber: prevHighestProposal,
		Success:               true,
		PreviouslyAcceptedProposalNumber: self.PreviouslyAcceptedProposalNumber,
		PreviouslyAcceptedValue:          self.PreviouslyAcceptedValue,
	}, nil
}

func (self *Paxos) AcceptValue(ctx context.Context, in *pr.AcceptRequest) (*pr.AcceptReply, error) {
	self.paxosStateLock.Lock()
	defer self.paxosStateLock.Unlock()

	// This node is not running paxos, cannot accept value
	if self.PaxosDone {
		return &pr.AcceptReply{
			Success:                  false,
			PreviouslyAcceptedNumber: self.PreviouslyAcceptedProposalNumber,
		}, nil
	}

	// if we have already accepted a larger proposal number, we have to reject, sorry :(
	if self.PreviouslyAcceptedProposalNumber > in.ProposalNumber {
		return &pr.AcceptReply{
			Success:                  false,
			PreviouslyAcceptedNumber: self.PreviouslyAcceptedProposalNumber,
		}, nil
	}

	//TODO: should we check the value to make sure it matches the previously accepted one?

	// we are accepting this!
	self.PreviouslyAcceptedProposalNumber = in.ProposalNumber
	self.PreviouslyAcceptedValue = in.ProposalValue
	return &pr.AcceptReply{Success: true}, nil
}

func (self *Paxos) Commit(ctx context.Context, in *pr.CommitRequest) (*pr.CommitReply, error) {
	self.paxosStateLock.Lock()
	defer self.paxosStateLock.Unlock()

	// if we are not running Paxos we obviously should not commit!
	if self.PaxosDone {
		return &pr.CommitReply{
			Success: false,
		}, nil
	}

	// commit these values
	self.CommitCallback(in.ProposalNumber, in.ProposalValue)
	self.PaxosDone = true
	self.PreviouslyAcceptedProposalNumber = 0
	self.PreviouslyAcceptedValue = 0

	// reply success
	return &pr.CommitReply{
		Success: true,
	}, nil
}

func (self *Paxos) Propose(item uint64) {

	// wait until paxos is done running to start this Propose
	for {
		self.paxosStateLock.Lock()
		if !self.PaxosDone {
			self.paxosStateLock.Unlock()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		self.PaxosDone = false
		self.paxosStateLock.Unlock()
		break
	}
	for {
		ourProposalNumber := self.HighestProposalNumber + 1
		success := self.paxosInitiateFirstStage(item, ourProposalNumber)
		if success {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (self *Paxos) paxosInitiateFirstStage(proposedValue uint64, ourProposalNumber uint64) bool {
	proposal := &pr.ProposeRequest{ProposalNumber: ourProposalNumber}

	// send proposal to every client
	proposalReplyChan := make(chan *pr.ProposeReply, len(self.ClientAddresses))
	missingValueReplyChan := make(chan uint64, len(self.ClientAddresses))
	var wg sync.WaitGroup
	for _, client := range self.ClientAddresses {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			conn, err := grpc.Dial(addr)
			defer conn.Close()
			if err != nil {
				fmt.Println("ERROR: %v", err)
				return
			}
			client := pr.NewPaxosRoutesClient(conn)
			reply, err := client.ProposeValue(context.Background(), proposal)
			if err != nil {
				fmt.Println("ERROR: %v", err)
				return
			}
			if reply.Success {
				proposalReplyChan <- reply
			} else if reply.MissingValue > 0 {

				// we must have missed some value, let's update it now
				missingValueReplyChan <- reply.MissingValue
			}
		}(client)
	}
	wg.Wait()

	// check if someone reported that this commit number already had an agreed value, if so we should use it and restart paxos
	select {
	case missing := <-missingValueReplyChan:
		self.CommitCallback(ourProposalNumber, missing)
		return false
	default:
	}

	var highestProposal *pr.ProposeReply = nil
	replyCount := 0
forloop:
	for {
		select {
		case proposal := <-proposalReplyChan:
			replyCount++
			if highestProposal == nil || proposal.PreviouslyAcceptedProposalNumber > highestProposal.PreviouslyAcceptedProposalNumber {
				highestProposal = proposal
			}
		default:
			break forloop
		}
	}

	// make sure a majority replied
	if replyCount < (len(self.ClientAddresses)/2 + 1) {
		if highestProposal != nil && highestProposal.PreviouslyAcceptedProposalNumber > self.HighestProposalNumber {
			self.paxosStateLock.Lock()
			self.HighestProposalNumber = highestProposal.HighestProposalNumber
			self.paxosStateLock.Unlock()
		}
		return false
	}
	return self.paxosInitiateSecondStage(highestProposal, ourProposalNumber, proposedValue)
}

func (self *Paxos) paxosInitiateSecondStage(largestPreviouslyAcceptedProposal *pr.ProposeReply, ourProposedNumber uint64, ourProposedValue uint64) bool {

	// if the highest proposal number reply from stage 1 contained a value, use it.
	// Otherwise use our initial value
	var valueToPropose uint64
	if largestPreviouslyAcceptedProposal.PreviouslyAcceptedProposalNumber > 0 {
		valueToPropose = largestPreviouslyAcceptedProposal.PreviouslyAcceptedValue
	} else {
		valueToPropose = ourProposedValue
	}

	// propose the highest known proposal + 1
	accept := &pr.AcceptRequest{
		ProposalNumber: ourProposedNumber,
		ProposalValue:  valueToPropose,
	}

	// send accept to every client
	acceptReplyChan := make(chan *pr.AcceptReply, len(self.ClientAddresses))
	var wg sync.WaitGroup
	for _, client := range self.ClientAddresses {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			conn, err := grpc.Dial(addr)
			defer conn.Close()
			if err != nil {
				fmt.Println("ERROR: %v", err)
				return
			}
			client := pr.NewPaxosRoutesClient(conn)
			reply, err := client.AcceptValue(context.Background(), accept)
			if err != nil {
				fmt.Println("ERROR: %v", err)
				return
			}
			if reply.Success {
				acceptReplyChan <- reply
			}
		}(client)
	}
	wg.Wait()
	replyCount := len(acceptReplyChan)

	// make sure a majority replied, if not, we have failed!
	if replyCount < (len(self.ClientAddresses)/2 + 1) {
		return false
	}
	return self.paxosInitiateFinalStage(ourProposedNumber, valueToPropose)
}

func (self *Paxos) paxosInitiateFinalStage(commitNumber uint64, commitValue uint64) bool {
	commit := &pr.CommitRequest{
		ProposalNumber: commitNumber,
		ProposalValue:  commitValue,
	}
	commitReplyChan := make(chan *pr.CommitReply, len(self.ClientAddresses))
	var wg sync.WaitGroup
	for _, client := range self.ClientAddresses {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			conn, err := grpc.Dial(addr)
			defer conn.Close()
			if err != nil {
				fmt.Println("ERROR: %v", err)
				return
			}
			client := pr.NewPaxosRoutesClient(conn)
			reply, err := client.Commit(context.Background(), commit)
			if err != nil {
				fmt.Println("ERROR: %v", err)
				return
			}
			if reply.Success {
				commitReplyChan <- reply
			}
		}(client)
	}
	wg.Wait()

	// Check if the majority committed
	// TODO: is this necessary
	if len(commitReplyChan) < (len(self.ClientAddresses)/2 + 1) {
		return false
	}
	return true
}
