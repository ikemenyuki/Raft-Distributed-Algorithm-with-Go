package raft

// 14-736 Lab 2 Raft implementation in go

import (
	"encoding/gob"
	"fmt"
	"strconv"

	"../remote"

	// "fmt"
	"math/rand"
	// "strconv"
	"sync"
	"time"
)

func init() {
	gob.Register([]LogCommand{})
	gob.Register(LogCommand{})
	gob.Register(StatusReport{})
}

// StatusReport struct sent from Raft node to Controller in response to command and status requests.
// this is needed by the Controller, so do not change it. make sure you give it to the Controller
// when requested
type StatusReport struct {
	Index     int
	Term      int
	Leader    bool
	CallCount int
}

func Min(x int, y int) int {
	if x <= y {
		return x
	}
	return y
}

// RaftInterface -- this is the "service interface" that is implemented by each Raft peer using the
// remote library from Lab 1.  it supports five remote methods that you must define and implement.
// these methods are described as follows:
//
//  1. RequestVote -- this is one of the remote calls defined in the Raft paper, and it should be
//     supported as such.  you will need to include whatever argument types are needed per the Raft
//     algorithm, and you can package the return values however you like, as long as the last return
//     type is `remote.RemoteObjectError`, since that is required for the remote library use.
//
//  2. AppendEntries -- this is one of the remote calls defined in the Raft paper, and it should be
//     supported as such and defined in a similar manner to RequestVote above.
//
//  3. GetCommittedCmd -- this is a remote call that is used by the Controller in the test code. it
//     allows the Controller to check the value of a commmitted log entry at a given index. the
//     type of the function is given below, and it must be implemented as given, otherwise the test
//     code will not function correctly.  more detail about this method is available later in this
//     starter code file.
//
//  4. GetStatus -- this is a remote call that is used by the Controller to collect status information
//     about the Raft peer.  the struct type that it returns is defined above, and it must be implemented
//     as given, or the Controller and test code will not function correctly.  more detail below.
//
//  5. NewCommand -- this is a remote call that is used by the Controller to emulate submission of
//     a new command value by a Raft client.  upon receipt, it will initiate processing of the command
//     and reply back to the Controller with a StatusReport struct as defined above. it must be
//     implemented as given, or the test code will not function correctly.  more detail below
type RaftInterface struct {
	RequestVote     func(int, int, int, int) (int, bool, remote.RemoteObjectError)                    // TODO: define function type
	AppendEntries   func(int, int, int, int, []LogCommand, int) (int, bool, remote.RemoteObjectError) // TODO: define function type
	GetCommittedCmd func(int) (int, remote.RemoteObjectError)
	GetStatus       func() (StatusReport, remote.RemoteObjectError)
	NewCommand      func(int) (StatusReport, remote.RemoteObjectError)
}

// you will need to define a struct that contains the parameters/variables that define and
// explain the current status of each Raft peer.  it doesn't matter what you call this struct,
// and the test code doesn't really care what state it contains, so this part is up to you.
// TODO: define a struct to maintain the local state of a single Raft peer
const TIMEOUT = 300
const addr string = "localhost:"
const (
	FOLLOWER  int = 0
	CANDIDATE int = 1
	LEADER    int = 2
)

const (
	SLEEP   = 0
	ACTIIVE = 1
)

type Raft struct {
	Lock          sync.Mutex
	Port          int
	RaftId        int
	CurrentTerm   int
	VotedFor      int
	Log           []LogCommand
	CommitIndex   int
	LastApplied   int
	PeerNum       int
	RemoteService *remote.Service
	State         int // Whether the server is activated or deactived
	Status        int // Status is one of FOLLOWER, CANDIDATE, or LEADER
	RemoteClients []RaftInterface
	Leader        int
	HeartBeat     chan bool
	Voted         chan bool
	VoteCount     int
	NextIdx       []int
	Won           chan bool
	MatchIdx      []int
	// AppliedEntry chan []LogCommand
}

type LogCommand struct {
	Command int
	Term    int
}

// `NewRaftPeer` -- this method should create an instance of the above struct and return a pointer
// to it back to the Controller, which calls this method.  this allows the Controller to create,
// interact with, and control the configuration as needed.  this method takes three parameters:
// -- port: this is the service port number where this Raft peer will listen for incoming messages
// -- id: this is the ID (or index) of this Raft peer in the peer group, ranging from 0 to num-1
// -- num: this is the number of Raft peers in the peer group (num > id)

func NewRaftPeer(port int, id int, num int) *Raft { // TODO: <---- change the return type
	// TODO: create a new raft peer and return a pointer to it
	newRaft := &Raft{
		Port:          port,
		RaftId:        id,
		CurrentTerm:   0,
		VotedFor:      -1,
		PeerNum:       num,
		Log:           make([]LogCommand, 0),
		Lock:          sync.Mutex{},
		State:         SLEEP,
		RemoteService: nil,
		Status:        FOLLOWER,
		HeartBeat:     make(chan bool),
		Voted:         make(chan bool),
		VoteCount:     0,
		Won:           make(chan bool),
		Leader:        -1,
		MatchIdx:      make([]int, 0),
		RemoteClients: make([]RaftInterface, num),
	}
	// when a new raft peer is created, its initial state should be populated into the corresponding
	// struct entries, and its `remote.Service` and `remote.StubFactory` components should be created,
	// but the Service should not be started (the Controller will do that when ready).
	//
	// the `remote.Service` should be bound to port number `port`, as given in the input argument.
	// each `remote.StubFactory` will be used to interact with a different Raft peer, and different
	// port numbers are used for each Raft peer.  the Controller assigns these port numbers sequentially
	// starting from peer with `id = 0` and ending with `id = num-1`, so any peer who knows its own
	// `id`, `port`, and `num` can determine the port number used by any other peer.
	// gob.Register(RaftServiceInterface{})
	// gob.Register(remote.RemoteObjectError{})
	gob.Register(StatusReport{})
	newRaft.RemoteService, _ = remote.NewService(&RaftInterface{}, newRaft, newRaft.Port, false, false)
	basePort := port - id
	for i := 0; i < num; i++ {
		if i == id {
			continue
		}
		stub := &newRaft.RemoteClients[i]
		err := remote.StubFactory(stub, addr+strconv.Itoa(basePort+i), false, false)
		if err != nil {
			fmt.Println(err.Error())
		}
	}
	for j := 0; j < num; j++ {
		newRaft.NextIdx = append(newRaft.NextIdx, 0)
		newRaft.MatchIdx = append(newRaft.MatchIdx, 0)
	}
	return newRaft
}

func (rf *Raft) Run() {
	for {
		if rf.State != ACTIIVE {
			break
		}
		if rf.Status == FOLLOWER {
			// using select statement, we are blocked until of of the channel is ready.
			select {
			case <-rf.Voted:
			case <-rf.HeartBeat:
			case <-time.After(time.Millisecond * time.Duration(TIMEOUT+rand.Intn(250))):
				rf.Lock.Lock()
				// vote for it self first
				rf.VotedFor = rf.RaftId
				rf.Status = CANDIDATE
				rf.Leader = -1
				rf.VoteCount = 1
				rf.Lock.Unlock()
			}
		} else if rf.Status == CANDIDATE {
			// we should send RequestVote RPC call to all peers and handle the feedbacks
			rf.BroadcastRequest()
			select {
			case <-rf.HeartBeat:
			case <-time.After(time.Millisecond * time.Duration(TIMEOUT+rand.Intn(250))):
			case <-rf.Won:
				// won the election, and send heartbeat message to everyone to establish its authority
				rf.Lock.Lock()
				rf.CurrentTerm += 1
				fmt.Printf("%d is the new Leader in term %d\n", rf.RaftId, rf.CurrentTerm)
				rf.Status = LEADER
				rf.Leader = rf.RaftId
				rf.VoteCount = 0
				rf.ResetChannel()
				rf.Lock.Unlock()
			}
		} else {
			rf.BoardcastAppend()
			if rf.Status != LEADER {
				continue
			}
			// If there exists an N such that N > commitIndex, a majority
			// of matchIndex[i] ≥ N, and log[N].term == currentTerm:
			// set commitIndex = N
			for N := len(rf.Log) - 1; N > rf.CommitIndex; N-- {
				count := 0
				if rf.Log[N].Term == rf.CurrentTerm {
					for j := range rf.MatchIdx {
						if rf.MatchIdx[j] >= N {
							count++
						}
					}
				}
				if count > rf.PeerNum/2 {
					rf.CommitIndex = N
					break
				}
			}
		}
	}
}

func (rf *Raft) BroadcastRequest() {
	if rf.Status != CANDIDATE {
		return
	}

	for i := range rf.RemoteClients {
		if i != rf.RaftId {
			go rf.SendRequest(i)
		}
	}
}

func (rf *Raft) SendRequest(i int) {
	rf.Lock.Lock()
	defer rf.Lock.Unlock()

	lastLogIdx := len(rf.Log) - 1
	var lastLogTerm int
	if lastLogIdx < 0 {
		// initial election
		lastLogIdx = 0
		lastLogTerm = 0
	} else {
		lastLogTerm = rf.Log[lastLogIdx].Term
	}

	retTerm, voted, err := rf.RemoteClients[i].RequestVote(rf.CurrentTerm, rf.RaftId, lastLogIdx, lastLogTerm)
	if err.Err != "" {
		fmt.Println(err.Error())
	}
	if retTerm > rf.CurrentTerm {
		//degrade itself to FOLLOWER Status
		rf.ResetChannel()
		rf.Status = FOLLOWER
	}
	if voted {
		rf.VoteCount += 1
	}
	if rf.VoteCount > rf.PeerNum/2 {
		rf.Won <- true

	}

}

func (rf *Raft) BoardcastAppend() {
	if rf.Status != LEADER {
		return
	}
	for i := range rf.RemoteClients {
		go rf.SendAppend(i)
	}
}

func (rf *Raft) SendAppend(i int) {
	rf.Lock.Lock()
	defer rf.Lock.Unlock()
	if rf.Status != LEADER {
		return
	}

	if i != rf.RaftId {
		var prevLogIdx int
		var prevLogTerm int
		prevLogIdx = rf.NextIdx[i]
		if len(rf.Log) == 0 {
			prevLogTerm = 0
		} else {
			prevLogTerm = rf.Log[prevLogIdx].Term
		}
		var logEntries []LogCommand = make([]LogCommand, 0)
		logEntries = rf.Log[prevLogIdx:]
		term, result, err := rf.RemoteClients[i].AppendEntries(rf.CurrentTerm, rf.RaftId, prevLogIdx, prevLogTerm, logEntries, rf.CommitIndex)
		if err.Err != "" {
			fmt.Println(err.Error())
			return
		}
		if !result {
			if term > rf.CurrentTerm {
				// the leader will step down as a FOLLOWER
				rf.Leader = -1
				rf.Status = FOLLOWER
				rf.ResetChannel()
			} else {
				// the peers need to be updated
				rf.NextIdx[i] -= 1
				if rf.NextIdx[i] < rf.MatchIdx[i] {
					fmt.Println("There is a bug, nextidx should be always greater than matchidx")
					return
				}
			}
		} else {
			// The peer has successfully replicated the log
			// should update the match index and the next index
			newMatch := prevLogIdx + len(logEntries)
			rf.MatchIdx[i] = newMatch
			rf.NextIdx[i] = newMatch + 1
		}

	}
}

func (rf *Raft) ResetChannel() {
	rf.HeartBeat = make(chan bool)
	rf.Voted = make(chan bool)
	rf.Won = make(chan bool)
}

func (rf *Raft) Activate() {
	// obj := &RaftServiceInterface{}
	// rf.RemoteService, _ = remote.NewService(&RaftInterface{}, obj, rf.Port, false, false)
	rf.State = ACTIIVE
	rf.RemoteService.Start()
	go rf.Run()
}

// `Activate` -- this method operates on your Raft peer struct and initiates functionality
// to allow the Raft peer to interact with others.  before the peer is activated, it can
// have internal algorithm state, but it cannot make remote calls using its stubs or receive
// remote calls using its underlying remote.Service interface.  in essence, when not activated,
// the Raft peer is "sleeping" from the perspective of any other Raft peer.
//
// this method is used exclusively by the Controller whenever it needs to "wake up" the Raft
// peer and allow it to start interacting with other Raft peers.  this is used to emulate
// connecting a new peer to the network or recovery of a previously failed peer.
//
// when this method is called, the Raft peer should do whatever is necessary to enable its
// remote.Service interface to support remote calls from other Raft peers as soon as the method
// returns (i.e., if it takes time for the remote.Service to start, this method should not
// return until that happens).  the method should not otherwise block the Controller, so it may
// be useful to spawn go routines from this method to handle the on-going operation of the Raft
// peer until the remote.Service stops.
//
// given an instance `rf` of your Raft peer struct, the Controller will call this method
// as `rf.Activate()`, so you should define this method accordingly. NOTE: this is _not_
// a remote call using the `remote.Service` interface of the Raft peer.  it uses direct
// method calls from the Controller, and is used purely for the purposes of the test code.
// you should not be using this method for any messaging between Raft peers.
//
// TODO: implement the `Activate` method
func (rf *Raft) Deactivate() {
	rf.State = SLEEP
	rf.RemoteService.Stop()
}

// `Deactivate` -- this method performs the "inverse" operation to `Activate`, namely to emulate
// disconnection / failure of the Raft peer.  when called, the Raft peer should effectively "go
// to sleep", meaning it should stop its underlying remote.Service interface, including shutting
// down the listening socket, causing any further remote calls to this Raft peer to fail due to
// connection error.  when deactivated, a Raft peer should not make or receive any remote calls,
// and any execution of the Raft protocol should effectively pause.  however, local state should
// be maintained, meaning if a Raft node was the LEADER when it was deactivated, it should still
// believe it is the leader when it reactivates.
//
// given an instance `rf` of your Raft peer struct, the Controller will call this method
// as `rf.Deactivate()`, so you should define this method accordingly. Similar notes / details
// apply here as with `Activate`
//
// TODO: implement the `Deactivate` method

// TODO: implement remote method calls from other Raft peers:
//
// RequestVote -- as described in the Raft paper, called by other Raft peers
func (rf *Raft) RequestVote(term int, candID int, lastLogIdx int, lastLogTerm int) (int, bool, remote.RemoteObjectError) {
	if term < rf.CurrentTerm {
		return rf.CurrentTerm, false, remote.RemoteObjectError{}
	}
	// change the state to follower if the RPC call's term > rf.CurrentTerm
	if term > rf.CurrentTerm {
		rf.Status = FOLLOWER
		rf.CurrentTerm = term
		rf.VotedFor = -1
		rf.ResetChannel()
	}
	// based on First Come First Vote principle, server will grant vote
	if len(rf.Log) == 0 {
		if rf.VotedFor < 0 || rf.VotedFor == candID {
			rf.VotedFor = candID
			rf.Voted <- true
			fmt.Printf("Raft Node %d grants vote to candidate %d \n", rf.RaftId, candID)
			return term, true, remote.RemoteObjectError{}
		}
	} else {
		isUpdated := rf.Check(lastLogIdx, lastLogTerm)
		if (rf.VotedFor < 0 || rf.VotedFor == candID) && isUpdated {
			fmt.Printf("Raft Node %d grants vote to candidate %d \n", rf.RaftId, candID)
			rf.VotedFor = candID
			rf.Voted <- true
			return term, true, remote.RemoteObjectError{}

		}
	}
	fmt.Printf("Raft Node %d does not grant vote to candidate %d \n", rf.RaftId, candID)
	return term, false, remote.RemoteObjectError{}
}

func (rf *Raft) Check(idx int, term int) bool {
	lastIdx := len(rf.Log) - 1
	lastTerm := rf.Log[lastIdx].Term
	if term > lastTerm {
		return true
	} else if term == lastTerm {
		return idx > lastIdx
	}
	return false
}

// AppendEntries -- as described in the Raft paper, called by other Raft peers
func (rf *Raft) AppendEntries(term int, leadId int, prevLogIdx int, prevLogTerm int, logEntries []LogCommand, leadComitIdx int) (int, bool, remote.RemoteObjectError) {
	rf.Lock.Lock()
	defer rf.Lock.Unlock()

	if term < rf.CurrentTerm {
		return rf.CurrentTerm, false, remote.RemoteObjectError{}
	}
	rf.HeartBeat <- true
	if term > rf.CurrentTerm {
		rf.Status = FOLLOWER
		rf.CurrentTerm = term
		rf.VotedFor = -1
		rf.ResetChannel()
	}
	rf.Leader = leadId
	if len(rf.Log)-1 < prevLogIdx || rf.Log[prevLogIdx].Term != prevLogTerm {
		return term, false, remote.RemoteObjectError{}
	}
	// passed the log term check append logs
	rf.Log = rf.Log[:prevLogIdx+1]
	for i := 0; i < len(logEntries); i++ {
		rf.Log = append(rf.Log, logEntries[i])
	}
	// check if leadComitIdx > rf.CommitIndex, if so, set min(leaderCommit, index of last new entry)
	if leadComitIdx > rf.CommitIndex {
		toComit := Min(leadComitIdx, len(rf.Log)-1)
		rf.CommitIndex = toComit
	}
	return term, true, remote.RemoteObjectError{}
}

// // function to handle commit
// func (rf *Raft) GoCommit() {
//     rf.Lock.Lock()
//     defer rf.Lock.Unlock()

// }

// GetCommittedCmd -- called (only) by the Controller.  this method provides an input argument
// `index`.  if the Raft peer has a log entry at the given `index`, and that log entry has been
// committed (per the Raft algorithm), then the command stored in the log entry should be returned
// to the Controller.  otherwise, the Raft peer should return the value 0, which is not a valid
// command number and indicates that no committed log entry exists at that index
func (rf *Raft) GetCommittedCmd(index int) (int, remote.RemoteObjectError) {
	// Check if the given index is within the range of the Log slice
	if index < 0 || index >= len(rf.Log) {
		err := remote.RemoteObjectError{Err: "index out of range"}
		return 0, err
	}

	// Check if the corresponding log entry has been committed
	if rf.CommitIndex >= index {
		// Return the command stored in the log entry
		return rf.Log[index].Command, remote.RemoteObjectError{}
	} else {
		// The log entry has not been committed
		return 0, remote.RemoteObjectError{Err: "log entry not committed"}
	}
}

// GetStatus -- called (only) by the Controller.  this method takes no arguments and is essentially
// a "getter" for the state of the Raft peer, including the Raft peer's current term, current last
// log index, role in the Raft algorithm, and total number of remote calls handled since starting.
// the method returns a `StatusReport` struct as defined at the top of this file.
func (rf *Raft) GetStatus() (StatusReport, remote.RemoteObjectError) {
	// get the remote service of raft peer
	serv := rf.RemoteService

	// return the StatusReport
	report := StatusReport{
		Term:      rf.CurrentTerm,
		Index:     len(rf.Log) - 1,
		Leader:    rf.Status == LEADER,
		CallCount: serv.GetCount(),
	}
	return report, remote.RemoteObjectError{}
}

// NewCommand -- called (only) by the Controller.  this method emulates submission of a new command
// by a Raft client to this Raft peer, which should be handled and processed according to the rules
// of the Raft algorithm.  once handled, the Raft peer should return a `StatusReport` struct with
// the updated status after the new command was handled.
func (rf *Raft) NewCommand(command int) (StatusReport, remote.RemoteObjectError) {
	// check if the callee raft peer is the leader
	if rf.Status != LEADER {
		roe := remote.RemoteObjectError{
			Err: "Not the Leader",
		}
		return StatusReport{}, roe
	}

	// broadcast logEntries to all raft peers
	logEntry := LogCommand{
		Term:    rf.CurrentTerm,
		Command: command,
	}
	rf.Log = append(rf.Log, logEntry)
	return rf.GetStatus()
	// curCmdIdx := len(rf.Log) - 1

	// // go routine to detect if CommitIndex is greater than or equal to the current appended log index
	// doneCh := make(chan struct{})
	// go func() {
	//     for {
	//         if rf.CommitIndex >= curCmdIdx {
	//             close(doneCh)
	//             return
	//         }
	//         time.Sleep(10 * time.Millisecond)
	//     }
	// }()

	// // Wait for the doneCh to be closed or for a timeout
	// select {
	// case <-doneCh:
	//     return rf.GetStatus()
	// case <-time.After(500 * time.Millisecond):
	//     roe := remote.RemoteObjectError{
	//         Err: "time out",
	//     }
	//     return StatusReport{}, roe
	// }

}

// general notes:
//
// - you are welcome to use additional helper to handle various aspects of the Raft algorithm logic
//   within the scope of a single Raft peer.  you should not need to create any additional remote
//   calls between Raft peers or the Controller.  if there is a desire to create additional remote
//   calls, please talk with the course staff before doing so.
//
// - please make sure to read the Raft paper (https://raft.github.io/raft.pdf) before attempting
//   any coding for this lab.  you will most likely need to refer to it many times during your
//   implementation and testing tasks, so please consult the paper for algorithm details.
//
// - each Raft peer will accept a lot of remote calls from other Raft peers and the Controller,
//   so use of locks / mutexes is essential.  you are expected to use locks correctly in order to
//   prevent race conditions in your implementation.  the Makefile supports testing both without
//   and with go's race detector, and the final auto-grader will enable the race detector, which will
//   cause tests to fail if any race conditions are encountered.
