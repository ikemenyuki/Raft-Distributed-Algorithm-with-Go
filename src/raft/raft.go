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

// StatusReport struct sent from Raft node to Controller in response to command and status requests.
// this is needed by the Controller, so do not change it. make sure you give it to the Controller
// when requested

func init() {
	gob.Register([]LogCommand{})
	gob.Register(LogCommand{})
	gob.Register(StatusReport{})
}

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
	VoteLock      sync.Mutex
	SendLock      sync.Mutex
	AppendLock    sync.Mutex
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
	failedNodes   []int
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
		VoteLock:      sync.Mutex{},
		State:         SLEEP,
		RemoteService: nil,
		Status:        FOLLOWER,
		HeartBeat:     make(chan bool),
		Voted:         make(chan bool),
		VoteCount:     0,
		Won:           make(chan bool),
		Leader:        -1,
		MatchIdx:      make([]int, num),
		NextIdx:       make([]int, num),
		RemoteClients: make([]RaftInterface, num),
		failedNodes:   make([]int, num),
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
	newRaft.RemoteService, _ = remote.NewService(&RaftInterface{}, newRaft, newRaft.Port, false, false)
	basePort := port - id
	for i := 0; i < num; i++ {
		newRaft.failedNodes[i] = 0
		newRaft.MatchIdx[i] = 0
		newRaft.NextIdx[i] = 0
		if i == id {
			continue
		}
		stub := &newRaft.RemoteClients[i]
		err := remote.StubFactory(stub, addr+strconv.Itoa(basePort+i), false, false)
		if err != nil {
			fmt.Println(err.Error())
		}
	}
	return newRaft
}

func (rf *Raft) CountDisconnect() int {
	sum := 0
	for i := 0; i < rf.PeerNum; i++ {
		sum += rf.failedNodes[i]
	}
	return sum
}

func (rf *Raft) ConvertToFollower(term int) {
	rf.Status = FOLLOWER
	rf.CurrentTerm = term
	rf.VotedFor = -1
	rf.Leader = -1
	for i := 0; i < rf.PeerNum; i++ {
		rf.failedNodes[i] = 0
	}
	rf.ResetChannel()
}

func max(x int, y int) int {
	if x > y {
		return x
	}
	return y
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
				rf.VoteLock.Lock()
				// vote for it self first
				rf.VotedFor = rf.RaftId
				rf.Status = CANDIDATE
				rf.Leader = -1
				rf.VoteCount = 1
				rf.VoteLock.Unlock()
			}
		} else if rf.Status == CANDIDATE {
			// we should send RequestVote RPC call to all peers and handle the feedbacks
			// fmt.Printf("%d is the Candidate now\n", rf.RaftId)
			rf.BroadcastRequest()
			select {
			case <-rf.HeartBeat:
			case <-time.After(time.Millisecond * time.Duration(150+rand.Intn(250))):
			case <-rf.Won:
				// won the election, and send heartbeat message to everyone to establish its authority
				rf.VoteLock.Lock()
				rf.CurrentTerm++
				fmt.Printf("%d is the new Leader in term %d\n", rf.RaftId, rf.CurrentTerm)
				rf.Status = LEADER
				rf.Leader = rf.RaftId
				rf.VoteCount = 0
				rf.ResetChannel()
				for i := 0; i < rf.PeerNum; i++ {
					rf.failedNodes[i] = 0
					rf.MatchIdx[i] = 0
					rf.NextIdx[i] = max(len(rf.Log)+1, 0)
				}
				rf.VoteLock.Unlock()
				rf.CurrentTerm += 1
			}
		} else {
			rf.BoardcastAppend()
			time.Sleep(time.Millisecond * 150)
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

func (rf *Raft) GetState() string {
	switch rf.Status {
	case LEADER:
		return "leader"
	case FOLLOWER:
		return "follower"
	default:
		return "candidate"
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
		// fmt.Println(err.Error())
		// if
		return
	}
	if retTerm > rf.CurrentTerm {
		//degrade itself to FOLLOWER Status
		rf.ConvertToFollower(retTerm)
		return
	}
	if voted {
		rf.VoteLock.Lock()
		rf.VoteCount += 1
		rf.VoteLock.Unlock()
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
		if i != rf.RaftId {
			go rf.SendAppend(i)
		}
	}
}

func (rf *Raft) SendAppend(i int) {
	if rf.Status != LEADER {
		return
	}

	if i != rf.RaftId {
		var prevLogIdx int
		var prevLogTerm int
		// prevLogIdx = rf.NextIdx[i] - 1
		prevLogIdx = len(rf.Log) - 1
		if len(rf.Log) == 0 {
			prevLogTerm = 0
		} else {
			// fmt.Printf("sending to node %d leader prevLogIdx is %d\n", i, prevLogIdx)
			// fmt.Println(rf.Log)
			prevLogTerm = rf.Log[prevLogIdx].Term
		}
		var logEntries []LogCommand = make([]LogCommand, 0)
		if prevLogTerm != len(rf.Log) {
			logEntries = rf.Log[prevLogIdx:]
		}

		term, result, err := rf.RemoteClients[i].AppendEntries(rf.CurrentTerm, rf.RaftId, prevLogIdx, prevLogTerm, logEntries, rf.CommitIndex)
		if err.Err != "" {
			// fmt.Println(err.Error())
			rf.failedNodes[i] = 1
			if rf.CountDisconnect() > rf.PeerNum/2 {
				rf.ConvertToFollower(rf.CurrentTerm)
			}
			return
		}
		if !result {
			if term > rf.CurrentTerm {
				// the leader will step down as a FOLLOWER
				rf.VoteLock.Lock()
				rf.Leader = -1
				rf.Status = FOLLOWER
				rf.ResetChannel()
				rf.VoteLock.Unlock()
			} else {
				// the peers need to be updated
				if rf.NextIdx[i] > 0 {
					rf.NextIdx[i] -= 1
					if rf.NextIdx[i] < rf.MatchIdx[i] {
						fmt.Println("There is a bug, nextidx should be always greater than matchidx")
						return
					}
				}
			}
		} else {
			// The peer has successfully replicated the log
			// should update the match index and the next index
			newMatch := prevLogIdx + len(logEntries)
			// fmt.Printf("node %d newMatch is %d, newNextIdx is %d\n", i, rf.MatchIdx[i], rf.NextIdx[i])
			// fmt.Printf("node %d prevLogIdx sent %d to node %d\n", rf.RaftId, prevLogIdx, i)
			// fmt.Println(rf.Log)
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
	fmt.Printf("%s index %d has been activated. It's current term is %d\n", rf.GetState(), rf.RaftId, rf.CurrentTerm)
	rf.ConvertToFollower(rf.CurrentTerm)
	rf.RemoteService.Start()
	go rf.Run()
}

func (rf *Raft) Deactivate() {
	rf.State = SLEEP
	rf.ConvertToFollower(rf.CurrentTerm)
	rf.RemoteService.Stop()
}

// RequestVote -- as described in the Raft paper, called by other Raft peers
func (rf *Raft) RequestVote(term int, candID int, lastLogIdx int, lastLogTerm int) (int, bool, remote.RemoteObjectError) {
	// 1. Reply false if term < currentTerm (§5.1)
	if term < rf.CurrentTerm {
		return rf.CurrentTerm, false, remote.RemoteObjectError{}
	}

	// Change the state to follower if the RPC call's term > rf.CurrentTerm
	if term > rf.CurrentTerm {
		rf.ConvertToFollower(term)
	}

	// 2. If votedFor is null or candidateId, and candidate’s log is at
	// least as up-to-date as receiver’s log, grant vote (§5.2, §5.4)
	if (rf.VotedFor == -1 || rf.VotedFor == candID) && rf.isLogUpToDate(lastLogIdx, lastLogTerm) {
		rf.VotedFor = candID
		// fmt.Printf("Raft Node %d grants vote to candidate %d \n", rf.RaftId, candID)
		return term, true, remote.RemoteObjectError{}
	}

	// fmt.Printf("Raft Node %d does not grant vote to candidate %d \n", rf.RaftId, candID)
	return term, false, remote.RemoteObjectError{}
}

// Check if the candidate's log is at least as up-to-date as the receiver's log
func (rf *Raft) isLogUpToDate(candidateLastLogIdx int, candidateLastLogTerm int) bool {
	if len(rf.Log) == 0 {
		return true
	}

	lastLogIndex := len(rf.Log) - 1
	lastLogTerm := rf.Log[lastLogIndex].Term

	// Compare the log terms
	if candidateLastLogTerm > lastLogTerm {
		return true
	} else if candidateLastLogTerm == lastLogTerm {
		// If the terms are equal, compare the log indices
		return candidateLastLogIdx >= lastLogIndex
	}

	// Candidate's log is not up-to-date
	return false
}

// func (rf *Raft) RequestVote(term int, candID int, lastLogIdx int, lastLogTerm int) (int, bool, remote.RemoteObjectError) {
// 	if term < rf.CurrentTerm {
// 		return rf.CurrentTerm, false, remote.RemoteObjectError{}
// 	}
// 	// change the state to follower if the RPC call's term > rf.CurrentTerm
// 	if term > rf.CurrentTerm {
// 		rf.ConvertToFollower(term)
// 		return term, false, remote.RemoteObjectError{}
// 	}
// 	// based on First Come First Vote principle, server will grant vote
// 	if len(rf.Log) == 0 {
// 		if rf.VotedFor < 0 || rf.VotedFor == candID {
// 			rf.VotedFor = candID
// 			rf.Voted <- true
// 			fmt.Printf("Raft Node %d grants vote to candidate %d \n", rf.RaftId, candID)
// 			return term, true, remote.RemoteObjectError{}
// 		} else {
// 			fmt.Println(strconv.Itoa(rf.RaftId) + " doesn't voted for " + strconv.Itoa(candID) + " it voted for " + strconv.Itoa(rf.VotedFor))
// 		}
// 	} else {
// 		isUpdated := rf.Check(lastLogIdx, lastLogTerm)
// 		if (rf.VotedFor < 0 || rf.VotedFor == candID) && isUpdated {
// 			fmt.Printf("Raft Node %d grants vote to candidate %d \n", rf.RaftId, candID)
// 			rf.VotedFor = candID
// 			rf.Voted <- true
// 			return term, true, remote.RemoteObjectError{}

// 		}
// 	}

// 	fmt.Printf("Raft Node %d does not grant vote to candidate %d \n", rf.RaftId, candID)
// 	return term, false, remote.RemoteObjectError{}
// }

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
	// rf.Lock.Lock()
	// defer rf.Lock.Unlock()
	if term < rf.CurrentTerm {
		return rf.CurrentTerm, false, remote.RemoteObjectError{}
	}
	rf.HeartBeat <- true
	if term > rf.CurrentTerm {
		rf.Status = FOLLOWER
	}
	rf.CurrentTerm = term
	rf.VotedFor = -1
	rf.Leader = leadId
	rf.ResetChannel()

	// check if it's a heartbeat
	if len(logEntries) == 0 {
		return term, true, remote.RemoteObjectError{}
	}

	// special condition when Raft peer log is empty
	if len(rf.Log) == 0 {
		for i := 0; i < len(logEntries); i++ {
			newEntry := LogCommand{
				Term:    logEntries[i].Term,
				Command: logEntries[i].Command,
			}
			rf.Log = append(rf.Log, newEntry)
		}
		return term, true, remote.RemoteObjectError{}
	}

	// fmt.Printf("%d lenlog, %d prevLgidx, %d prevLgTm\n", len(rf.Log), prevLogIdx, prevLogTerm)
	if len(rf.Log)-1 < prevLogIdx || rf.Log[prevLogIdx].Term != prevLogTerm {
		return term, false, remote.RemoteObjectError{}
	}
	// passed the log term check append logs
	rf.Log = rf.Log[:prevLogIdx+1]
	for i := 0; i < len(logEntries); i++ {
		newEntry := LogCommand{
			Term:    logEntries[i].Term,
			Command: logEntries[i].Command,
		}
		rf.Log = append(rf.Log, newEntry)
	}
	// check if leadComitIdx > rf.CommitIndex, if so, set min(leaderCommit, index of last new entry)
	if leadComitIdx > rf.CommitIndex {
		toComit := Min(leadComitIdx, len(rf.Log)-1)
		rf.CommitIndex = toComit
	}
	if len(logEntries) > 0 {
		// fmt.Printf("node %d received successesfully, new Log is ", rf.RaftId)
		fmt.Println(rf.Log)
	}
	return term, true, remote.RemoteObjectError{}
}

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
	fmt.Printf("\nnew command: LOG appended, now leader log is ")
	fmt.Println(rf.Log)
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
