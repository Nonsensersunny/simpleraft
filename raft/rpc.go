package raft

type VoteArgs struct {
	Term        int
	CandidateID int
}

type VoteResp struct {
	Term        int
	VoteGranted bool
}

type HeartbeatArgs struct {
	Term     int
	LeaderID int

	PrevLogIndex    int
	PrevLogTerm     int
	Entries         []LogEntry
	LeaderCommitted int
}

type HeartbeatResp struct {
	Term int

	Success   bool
	NextIndex int
}
