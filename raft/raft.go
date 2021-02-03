package raft

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/rpc"
	"time"
)

type Node struct {
	connect bool
	address string
}

type State int

const (
	Follower State = iota
	Candidate
	Leader
)

type LogEntry struct {
	LogTerm  int
	LogIndex int
	LogCMD   interface{}
}

type Raft struct {
	Id          int
	Nodes       map[int]*Node
	state       State
	currentTerm int
	votedFor    int
	voteCount   int
	heartbeatC  chan bool
	toLeaderC   chan bool

	log         []LogEntry
	commitIndex int
	lastApplied int
	nextIndex   []int
	matchIndex  []int
}

func NewNode(addr string) *Node {
	return &Node{
		address: addr,
	}
}

func (rf *Raft) Start() {
	rf.state = Follower
	rf.currentTerm = 0
	rf.votedFor = -1
	rf.heartbeatC = make(chan bool)
	rf.toLeaderC = make(chan bool)

	go func() {
		rand.Seed(time.Now().UnixNano())

		for {
			switch rf.state {
			case Follower:
				select {
				case <-rf.heartbeatC:
					log.Printf("follower: %v received heartbeat", rf.Id)
				case <-time.After(time.Duration(rand.Intn(500-300)+300) * time.Millisecond):
					log.Printf("follower: %v timeout", rf.Id)
					rf.state = Candidate
				}
			case Candidate:
				log.Printf("Node:%v becomes candidate", rf.Id)
				rf.currentTerm++
				rf.votedFor = rf.Id
				rf.voteCount = 1

				go rf.broadcastRequestVote()

				select {
				case <-time.After(time.Duration(rand.Intn(500-300)+300) * time.Millisecond):
					rf.state = Follower
				case <-rf.toLeaderC:
					log.Printf("Node:%v becomes leader", rf.Id)
					rf.state = Leader

					rf.nextIndex = make([]int, len(rf.Nodes))
					rf.matchIndex = make([]int, len(rf.Nodes))
					for i := range rf.Nodes {
						rf.nextIndex[i] = 1
						rf.matchIndex[i] = 0
					}

					go func() {
						i := 0
						for {
							i++
							rf.log = append(rf.log, LogEntry{
								LogTerm:  rf.currentTerm,
								LogIndex: i,
								LogCMD:   fmt.Sprintf("sending cmd: %v", i),
							})
							time.Sleep(time.Second * 3)
						}
					}()
				}
			case Leader:
				rf.broadcastHeartbeat()
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()
}

func (rf *Raft) broadcastRequestVote() {
	args := VoteArgs{
		Term:        rf.currentTerm,
		CandidateID: rf.Id,
	}

	for i := range rf.Nodes {
		go func(i int) {
			var resp VoteResp
			rf.sendRequestVote(i, args, &resp)
		}(i)
	}
}

func (rf *Raft) sendRequestVote(i int, args VoteArgs, resp *VoteResp) {
	client, err := rpc.DialHTTP("tcp", rf.Nodes[i].address)
	if err != nil {
		log.Fatalf("request vote dialing err:%v", err)
	}
	defer client.Close()

	client.Call("Raft.RequestVote", args, resp)

	if resp.Term > rf.currentTerm {
		rf.currentTerm = resp.Term
		rf.state = Follower
		rf.votedFor = -1
		return
	}

	if resp.VoteGranted {
		rf.voteCount++
	}
	if rf.voteCount >= len(rf.Nodes)/2+1 {
		rf.toLeaderC <- true
	}
}

func (rf *Raft) RequestVote(args VoteArgs, resp *VoteResp) error {
	if args.Term < rf.currentTerm {
		resp.Term = rf.currentTerm
		resp.VoteGranted = false
		return nil
	}

	if rf.votedFor == -1 {
		rf.currentTerm = args.Term
		rf.votedFor = args.Term
		resp.Term = rf.currentTerm
		resp.VoteGranted = true
		return nil
	}

	resp.Term = rf.currentTerm
	resp.VoteGranted = false
	return nil
}

func (rf *Raft) broadcastHeartbeat() {
	for i := range rf.Nodes {
		args := HeartbeatArgs{
			Term:            rf.currentTerm,
			LeaderID:        rf.Id,
			LeaderCommitted: rf.commitIndex,
		}

		prevLogIndex := rf.nextIndex[i] - 1
		if rf.getLastIndex() > prevLogIndex {
			args.PrevLogIndex = prevLogIndex
			args.PrevLogTerm = rf.log[prevLogIndex].LogTerm
			args.Entries = rf.log[prevLogIndex:]
			log.Printf("sending entries, %+v", args.Entries)
		}
		go func(i int, args HeartbeatArgs) {
			var resp HeartbeatResp
			rf.sendHeartbeat(i, args, &resp)
		}(i, args)
	}
}

func (rf *Raft) sendHeartbeat(i int, args HeartbeatArgs, resp *HeartbeatResp) {
	client, err := rpc.DialHTTP("tcp", rf.Nodes[i].address)
	if err != nil {
		log.Fatalf("heartbeat dialinf err:%v", err)
	}
	defer client.Close()

	client.Call("Raft.Heartbeat", args, resp)

	if resp.Success {
		if resp.NextIndex > 0 {
			rf.nextIndex[i] = resp.NextIndex
			rf.matchIndex[i] = rf.nextIndex[i] - 1

			// TODO
			/**
			if log succeeded on more major nodes:
			1. update commitIndex on leader
			2. respond success to client
			3. apply status machine
			4. broadcast to followers that log applied
			*/
		}
	} else {
		if resp.Term > rf.currentTerm {
			rf.currentTerm = resp.Term
			rf.state = Follower
			rf.votedFor = -1
		}
	}
}

func (rf *Raft) Heartbeat(args HeartbeatArgs, resp *HeartbeatResp) error {
	if args.Term < rf.currentTerm {
		resp.Term = rf.currentTerm
		return nil
	}

	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = Follower
		rf.votedFor = -1
	}

	resp.Term = rf.currentTerm

	rf.heartbeatC <- true

	if len(args.Entries) < 1 {
		resp.Success = true
		resp.Term = rf.currentTerm
		return nil
	}

	if args.PrevLogIndex > rf.getLastIndex() {
		resp.Success = false
		resp.Term = rf.currentTerm
		resp.NextIndex = rf.getLastIndex() + 1
		return nil
	}

	rf.log = append(rf.log, args.Entries...)
	rf.commitIndex = rf.getLastIndex()

	resp.Success = true
	resp.Term = rf.currentTerm
	resp.NextIndex = rf.getLastIndex() + 1

	return nil
}

func (rf *Raft) RPC(port int) {
	rpc.Register(rf)
	rpc.HandleHTTP()

	l, err := net.Listen("tcp", fmt.Sprintf("localhost:%v", port))
	if err != nil {
		panic(err)
	}

	go http.Serve(l, nil)
}

func (rf *Raft) getLastIndex() int {
	rl := len(rf.log)
	if rl == 0 {
		return rl
	}
	return rf.log[rl-1].LogIndex
}

func (rf *Raft) PrintLog() {
	go func() {
		for {
			log.Printf("node:%v, logs:%+v", rf.Id, rf.log)
			time.Sleep(time.Second * 5)
		}
	}()
}
