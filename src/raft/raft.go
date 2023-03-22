package raft

// 14-736 Lab 2 Raft implementation in go

import (
	"encoding/gob"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"../remote"
)

// Register the LogCommand and StatusReport types with the Gob encoder
func init() {
	gob.Register([]LogCommand{})
	gob.Register(LogCommand{})
	gob.Register(StatusReport{})
}

// The StatusReport struct represents the status of a Raft server.
type StatusReport struct {
	Index     int  // The index of the latest entry in the Raft log.
	Term      int  // The current term of the Raft server.
	Leader    bool // Whether this server is currently the leader.
	CallCount int  // The number of times the leader has made a call to a follower.
}

func min(x int, y int) int {
	if x <= y {
		return x
	}
	return y
}

// Define the RaftInterface type, which specifies the functions that can be called remotely
type RaftInterface struct {
	RequestVote     func(int, int, int, int) (int, bool, remote.RemoteObjectError)
	AppendEntries   func(int, int, int, int, []LogCommand, int) (int, bool, remote.RemoteObjectError)
	GetCommittedCmd func(int) (int, remote.RemoteObjectError)
	GetStatus       func() (StatusReport, remote.RemoteObjectError)
	NewCommand      func(int) (StatusReport, remote.RemoteObjectError)
}

// Define some constants that are used throughout the code
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

// The Raft struct represents a single Raft server.
type Raft struct {
	Mu            sync.Mutex      // A mutex to protect the Raft struct's fields.
	Port          int             // The port number that this Raft server listens on.
	RaftId        int             // The unique ID of this Raft server.
	CurrentTerm   int             // The latest term that this Raft server has seen.
	VotedFor      int             // The ID of the candidate that this server has voted for in the current term, or -1 if none.
	Log           []LogCommand    // The Raft log.
	CommitIndex   int             // The index of the highest log entry that has been committed.
	LastApplied   int             // The index of the highest log entry that has been applied to the server's state machine.
	PeerNum       int             // The number of peers in the Raft cluster.
	RemoteService *remote.Service // The RPC service used to communicate with other Raft servers.
	State         int             // Whether the server is activated or deactived (SLEEP or ACTIVE).
	Status        int             // Whether the server is a follower, candidate, or leader.
	RemoteClients []RaftInterface // A list of clients that this Raft server can contact to make RPC calls.
	Leader        int             // The ID of the current leader, or -1 if there is no leader.
	HeartBeat     chan bool       // A channel that is used to send heartbeats to other Raft servers.
	Voted         chan bool       // A channel that is used to signal that this server has received a vote from another server.
	VoteCount     int             // The number of votes that this server has received in the current term.
	NextIdx       []int           // The next index to send to each server when sending AppendEntries RPCs.
	Won           chan bool       // A channel that is used to signal that this server has won the election.
	MatchIdx      []int           // The index of the highest log entry known to be replicated on each server.
	StopCh        chan bool       // A channel that is used to signal that the server should stop.
	// WonBool       bool            // Whether this server has won the election.
}

// The LogCommand struct represents a single command in the Raft log.
type LogCommand struct {
	Command int // The command to be executed.
	Term    int // The term in which the command was received by the leader.
}

func NewRaftPeer(port int, id int, num int) *Raft {
	newRaft := &Raft{
		Port:          port,
		RaftId:        id,
		CurrentTerm:   0,
		VotedFor:      -1,
		PeerNum:       num,
		Log:           make([]LogCommand, 0),
		State:         SLEEP,
		RemoteService: nil,
		Status:        FOLLOWER,
		HeartBeat:     make(chan bool),
		Voted:         make(chan bool, 1),
		VoteCount:     0,
		Won:           make(chan bool),
		Leader:        -1,
		MatchIdx:      make([]int, num),
		NextIdx:       make([]int, num),
		RemoteClients: make([]RaftInterface, num),
		StopCh:        make(chan bool),
		// WonBool:       false,
	}
	newRaft.RemoteService, _ = remote.NewService(&RaftInterface{}, newRaft, newRaft.Port, false, false)
	basePort := port - id
	for i := 0; i < num; i++ {
		newRaft.MatchIdx[i] = 0
		newRaft.NextIdx[i] = 0
		if i == id {
			continue
		}
		stub := &newRaft.RemoteClients[i]
		err := remote.StubFactory(stub, addr+strconv.Itoa(basePort+i), false, false)
		if err != nil {
			// fmt.Println(err.Error())
		}
	}
	return newRaft
}

// lock must be used before calling this
func (rf *Raft) GetLastIndex() int {
	return len(rf.Log) - 1
}

// lock must be used before calling this
func (rf *Raft) GetLastTerm() int {
	if len(rf.Log) == 0 {
		return 0
	}
	lastLogIdx := rf.GetLastIndex()
	return rf.Log[lastLogIdx].Term
}

// This function convert any server to a FOLLOWER status in Raft. A lock must
// be used before this function
func (rf *Raft) ConvertToFollower(term int) {
	rf.Status = FOLLOWER
	rf.CurrentTerm = term
	rf.VotedFor = -1
	rf.Leader = -1
	rf.VoteCount = 0
	// rf.WonBool = false
}

// This function convert any server to a LEADER status in Raft. A lock must
// be used before this function
func (rf *Raft) ConvertToLeader() {
	// fmt.Printf("Node %d is the leader now\n", rf.RaftId)
	// change leader status
	rf.Status = LEADER
	rf.Leader = rf.RaftId
	// empty vote status
	rf.VoteCount = 0
	rf.VotedFor = -1
	// rf.WonBool = false
	// reset NextIdx & MatchIdx
	for i := 0; i < rf.PeerNum; i++ {
		if i != rf.RaftId {
			rf.NextIdx[i] = rf.GetLastIndex() + 1
			rf.MatchIdx[i] = 0
		}
	}
}

// This function convert any server to a CANDIDATE status in Raft. A lock must
// be used before this function
func (rf *Raft) ConvertToCandidate() {
	// increment term
	// fmt.Printf("node %d is a condiate now\n", rf.RaftId)
	rf.CurrentTerm++
	rf.Status = CANDIDATE
	rf.VotedFor = rf.RaftId
	rf.VoteCount = 1
	rf.Leader = -1
	// rf.WonBool = false
}

func (rf *Raft) Run() {
	for {
		rf.Mu.Lock()
		status := rf.Status
		rf.Mu.Unlock()
		select {
		case <-rf.StopCh:
			return
		default:
			switch status {
			case FOLLOWER:
				select {
				case <-rf.HeartBeat:
				case <-rf.Voted:
				case <-time.After(time.Millisecond * time.Duration(TIMEOUT+rand.Intn(250))):
					// times up, need to become candidate
					rf.Mu.Lock()
					rf.ConvertToCandidate()
					rf.Mu.Unlock()
				}
			case CANDIDATE:
				// fmt.Println("Node ", rf.RaftId, " claims to be a candidate")
				rf.BroadcastRequest()
				select {
				case <-rf.HeartBeat:
					rf.Mu.Lock()
					rf.ConvertToFollower(rf.CurrentTerm)
					rf.Mu.Unlock()
				case <-rf.Won:
					// fmt.Println("Node ", rf.RaftId, " won the election")
					rf.Mu.Lock()
					rf.ConvertToLeader()
					rf.Mu.Unlock()
				case <-time.After(time.Millisecond * time.Duration(TIMEOUT+rand.Intn(250))):
					rf.Mu.Lock()
					rf.ConvertToCandidate()
					rf.Mu.Unlock()
				}
			case LEADER:
				// send broadcast append messages / heartbeat
				rf.BroadcastAppend()
				// sleep for a while
				time.Sleep(150 * time.Millisecond)
			}
		}
		rf.Mu.Lock()
		if rf.State != ACTIIVE {
			rf.Mu.Unlock()
			break
		}
		rf.Mu.Unlock()
		// run server according to their status
	}
}

// This function is called by the LEADER only. After leader broadcast AppendEntries RPC call to
// its followers, it is expected to update the commit index.
// The implementation algorithm is based on following details from the Raft paper:
//
//	"If there exists an N such that N > commitIndex, a majority
//	of matchIndex[i] ≥ N, and log[N].term == currentTerm:
//	set commitIndex = N (§5.3, §5.4)"
//
// Note that a lock also must be applied before calling this function.
func (rf *Raft) UpdateCommitIndex() {
	for iter := rf.CommitIndex; iter < len(rf.Log); iter++ {
		if rf.Log[iter].Term < rf.CurrentTerm {
			continue
		}
		count := 1
		for j := 0; j < rf.PeerNum; j++ {
			if j != rf.RaftId {
				if rf.MatchIdx[j] > iter {
					count++
				}
			}
		}
		if count*2 > rf.PeerNum {
			for i := rf.CommitIndex; i <= iter; i++ {
				rf.CommitIndex = i + 1
			}
		} else { // commit in order
			break
		}
	}
}

// This function is called by CANDIDATE for election only. A CANDIDATE will call SendRequest function
// to all the other peers to obtain their votes. Inside a for-loop, gorountines will be created for each
// peer to call each SendRequest function.
func (rf *Raft) BroadcastRequest() {
	// rf.Mu.Lock()
	status := rf.Status
	peerNum := rf.PeerNum
	raftId := rf.RaftId
	// rf.Mu.Unlock()
	if status != CANDIDATE {
		return
	}
	for i := 0; i < peerNum; i++ {
		if i != raftId {
			go rf.SendRequest(i)
		}
	}
}

// This function is called inside BroadcastRequest function in goroutines. This function will call the
// RequestVote function, which includes the CANDIDATE's currTerm, candId, lastLogIdx, lastLogTerm.
// Also, this function will collect the vote result of each peer: it will count its current vote,
// and if the vote count is greater than half of the peer num in side the Raft Network, it will signal
// the main thread that it has won the election.
func (rf *Raft) SendRequest(i int) {
	rf.Mu.Lock()
	if rf.Status != CANDIDATE {
		rf.Mu.Unlock()
		return
	}
	currTerm := rf.CurrentTerm
	candId := rf.RaftId
	lastLogIdx := rf.GetLastIndex()
	lastLogTerm := rf.GetLastTerm()
	remoteClient := rf.RemoteClients[i]
	rf.Mu.Unlock()
	term, success, err := remoteClient.RequestVote(currTerm, candId, lastLogIdx, lastLogTerm)

	// rf.Mu.Lock()
	if err.Err != "" {
		// rf.Mu.Unlock()
		return
	}
	// if term > currTerm, convert to follower
	if term > currTerm {
		rf.Mu.Lock()
		rf.ConvertToFollower(term)
		rf.Mu.Unlock()
		return
	}
	// if success, increment and count how many votes
	if success {
		rf.Mu.Lock()
		voteCount := rf.VoteCount + 1
		rf.VoteCount++
		expectedPeerNum := rf.PeerNum / 2
		rf.Mu.Unlock()
		if voteCount > expectedPeerNum {
			rf.Won <- true
		}
	}
	// rf.Mu.Unlock()
}

// This function is called by LEADER for heartbeat and update log entries only. For heartbeat purposes,
// when a CANDIDATE won the election, it will send a heartbeat (empty log entries) to every other server
// to signal them that it won the election, forcing them to return to the FOLLOWER state; also, if no command
// is added, it will also send heartbeat to prevent election timeout. If there are new log entries to be committed,
// it will send new entries and wait peers' return messages.
// This function will create a goroutine for each peer.
func (rf *Raft) BroadcastAppend() {
	rf.Mu.Lock()
	status := rf.Status
	peerNum := rf.PeerNum
	raftId := rf.RaftId
	rf.Mu.Unlock()
	if status != LEADER {
		return
	}
	for i := 0; i < peerNum; i++ {
		if i != raftId {
			go rf.SendAppend(i)
		}
	}
	// update commit index
	rf.Mu.Lock()
	defer rf.Mu.Unlock()
	rf.UpdateCommitIndex()
}

// This function is called inside BroadcastAppend function. It calls the AppendEntries RPC function to either
// send a heartbeat or to update log entries to its followers.
func (rf *Raft) SendAppend(i int) {
	rf.Mu.Lock()
	if rf.Status != LEADER {
		rf.Mu.Unlock()
		return
	}
	// send append messages to peers
	currentTerm := rf.CurrentTerm
	prevLogIdx := rf.NextIdx[i] - 1 // if it's -1, it means receiver need to accept all sender's log entry
	prevLogTerm := 0
	leaderId := rf.RaftId
	commitIdx := rf.CommitIndex
	logEntry := make([]LogCommand, 0)
	// check what to send
	if prevLogIdx >= 0 {
		prevLogTerm = rf.Log[prevLogIdx].Term
	}
	// prepare log entries to send
	lastLogIdx := rf.GetLastIndex()
	if lastLogIdx >= rf.NextIdx[i] && rf.NextIdx[i] >= 0 {
		logEntry = rf.Log[rf.NextIdx[i]:]
	}
	remoteClient := rf.RemoteClients[i]
	rf.Mu.Unlock()
	term, success, err := remoteClient.AppendEntries(currentTerm, leaderId, prevLogIdx, prevLogTerm, logEntry, commitIdx)
	// if error connecting peer, return
	if err.Err != "" {
		return
	}

	rf.Mu.Lock()
	if term > currentTerm {
		rf.ConvertToFollower(term)
		rf.Mu.Unlock()
		return
	}
	// if it's heartbeat and passed the term check, it's guaranteed to succeed
	if len(logEntry) == 0 {
		rf.Mu.Unlock()
		return
	}
	// if not success, decrement nextIdx
	if !success {
		rf.NextIdx[i]--
		rf.Mu.Unlock()
		return
	}
	// else increase nextIdx and matchIdx
	newMatch := prevLogIdx + len(logEntry) + 1
	rf.MatchIdx[i] = newMatch
	rf.NextIdx[i] = newMatch
	rf.Mu.Unlock()
}

// The RPC function called by Raft Controller only to activate a Raft Server.
func (rf *Raft) Activate() {
	rf.Mu.Lock()
	// fmt.Println("Node ", rf.RaftId, " is Activated")
	rf.State = ACTIIVE
	rf.ConvertToFollower(rf.CurrentTerm)
	rf.RemoteService.Start()
	rf.Won = make(chan bool)
	rf.Mu.Unlock()
	go rf.Run()
}

// The RPC function called by Raft Controller only to deactivate a Raft Server.
func (rf *Raft) Deactivate() {
	rf.Mu.Lock()
	// fmt.Println("Node ", rf.RaftId, " is Dectivated")
	rf.State = SLEEP
	rf.ConvertToFollower(rf.CurrentTerm)
	rf.RemoteService.Stop()
	rf.Won = make(chan bool)
	// fmt.Println("Node ", rf.RaftId, " successfully killed")
	rf.Mu.Unlock()
	select {
	case rf.StopCh <- true:
	default:
	}
}

// RequestVote -- as described in the Raft paper, called by other Raft peer. When it is called,
// the receiver of the the RPC call will determine whether to grant vote to the caller based on the
// following algorithm as descriped in the Raft paper.
//
//	"1. Reply false if term < currentTerm (§5.1)
//	 2. If votedFor is null or candidateId, and candidate’s log is at
//		least as up-to-date as receiver’s log, grant vote (§5.2, §5.4)""
func (rf *Raft) RequestVote(term int, candID int, candLastLogIdx int, candLastLogTerm int) (int, bool, remote.RemoteObjectError) {
	// 1. if term < rf.CurrTerm, reject the caller
	rf.Mu.Lock()
	// fmt.Println("Node ", rf.RaftId, " receives requestVote from ", candID)
	if term < rf.CurrentTerm {
		// fmt.Println("Node ", rf.RaftId, " does not grant vote to ", candID)
		curTerm := rf.CurrentTerm
		rf.Mu.Unlock()
		return curTerm, false, remote.RemoteObjectError{}
	} else if term > rf.CurrentTerm {
		// convert to follower
		rf.ConvertToFollower(term)
	}

	// 2. If votedFor is null or candidateId, and candidate’s log is at
	// least as up-to-date as receiver’s log, grant vote (§5.2, §5.4)
	if rf.VotedFor == -1 && rf.isLogUpToDate(candLastLogIdx, candLastLogTerm) {
		// fmt.Println("Node ", rf.RaftId, " grants vote to ", candID)
		rf.VotedFor = candID
		rf.Mu.Unlock()
		rf.Voted <- true
		return term, true, remote.RemoteObjectError{}
	}
	// fmt.Println("Node ", rf.RaftId, " does not grant vote to ", candID)
	rf.Mu.Unlock()
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

// AppendEntries -- as described in the Raft paper, called by other Raft peers. The receiver will determine whether to
// update the log entries based on the following algorithm discussed in the paper:
//
//	"1. Reply false if term < currentTerm (§5.1)
//	 2. Reply false if log doesn’t contain an entry at prevLogIndex
//		whose term matches prevLogTerm (§5.3)
//	 3. If an existing entry conflicts with a new one (same index
//		but different terms), delete the existing entry and all that
//		follow it (§5.3)
//	 4. Append any new entries not already in the log
//	 5. If leaderCommit > commitIndex, set commitIndex =
//		min(leaderCommit, index of last new entry)""
func (rf *Raft) AppendEntries(term int, leadId int, prevLogIdx int, prevLogTerm int, logEntries []LogCommand, leadComitIdx int) (int, bool, remote.RemoteObjectError) {
	rf.Mu.Lock()
	// check term
	if term < rf.CurrentTerm {
		curTerm := rf.CurrentTerm
		rf.Mu.Unlock()
		return curTerm, false, remote.RemoteObjectError{}
	}
	// send heartbeat channel
	rf.Mu.Unlock()
	rf.HeartBeat <- true
	// convert to follower
	rf.Mu.Lock()
	defer rf.Mu.Unlock()
	rf.ConvertToFollower(term)
	logMatched := false
	if prevLogIdx == -1 {
		logMatched = true
	} else {
		if len(rf.Log) > prevLogIdx && prevLogTerm == rf.Log[prevLogIdx].Term {
			logMatched = true
		}
	}
	// if log not matched, return false
	if !logMatched {
		return rf.CurrentTerm, false, remote.RemoteObjectError{}
	}
	rf.Log = rf.Log[:prevLogIdx+1]
	// log matched, append log entries
	for i := 0; i < len(logEntries); i++ {
		newCommand := LogCommand{
			Command: logEntries[i].Command,
			Term:    logEntries[i].Term,
		}
		rf.Log = append(rf.Log, newCommand)
	}
	// update peer commit index
	if leadComitIdx > rf.CommitIndex {
		toComit := min(leadComitIdx, len(rf.Log))
		rf.CommitIndex = toComit
	}
	// fmt.Printf("Node %d appended log from leader %d, now the log entry is", rf.RaftId, leadId)
	return rf.CurrentTerm, true, remote.RemoteObjectError{}
}

// GetCommittedCmd -- called (only) by the Controller.  this method provides an input argument
// `index`.  if the Raft peer has a log entry at the given `index`, and that log entry has been
// committed (per the Raft algorithm), then the command stored in the log entry should be returned
// to the Controller.  otherwise, the Raft peer should return the value 0, which is not a valid
// command number and indicates that no committed log entry exists at that index
func (rf *Raft) GetCommittedCmd(index int) (int, remote.RemoteObjectError) {
	rf.Mu.Lock()
	defer rf.Mu.Unlock()
	// Check if the given index is within the range of the Log slice
	if index < 1 || index > len(rf.Log) {
		err := remote.RemoteObjectError{Err: "index out of range"}
		return 0, err
	}
	if rf.CommitIndex >= index {
		// Return the command stored in the log entry
		return rf.Log[index-1].Command, remote.RemoteObjectError{}
	} else {
		// The log entry has not been committed
		return 0, remote.RemoteObjectError{Err: "entry not committed"}
	}
}

// GetStatus -- called (only) by the Controller.  this method takes no arguments and is essentially
// a "getter" for the state of the Raft peer, including the Raft peer's current term, current last
// log index, role in the Raft algorithm, and total number of remote calls handled since starting.
// the method returns a `StatusReport` struct as defined at the top of this file.
func (rf *Raft) GetStatus() (StatusReport, remote.RemoteObjectError) {
	// get the remote service of raft peer
	rf.Mu.Lock()
	defer rf.Mu.Unlock()
	serv := rf.RemoteService
	// return the StatusReport
	report := StatusReport{
		Term:      rf.CurrentTerm,
		Index:     len(rf.Log),
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
	rf.Mu.Lock()
	defer rf.Mu.Unlock()
	status := rf.Status

	if status != LEADER {
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
	// fmt.Println("new command ", command, " new leader log is ", rf.Log)
	rf.MatchIdx[rf.RaftId]++
	serv := rf.RemoteService
	// return the StatusReport
	report := StatusReport{
		Term:      rf.CurrentTerm,
		Index:     len(rf.Log),
		Leader:    rf.Status == LEADER,
		CallCount: serv.GetCount(),
	}
	return report, remote.RemoteObjectError{}
}
