// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package raft

import (
	"fmt"
	"github.com/pingcap-incubator/tinykv/log"
	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

// RaftLog manage the log entries, its struct look like:
//
//  snapshot/first.....applied....committed....stabled.....last
//  --------|------------------------------------------------|
//                            log entries
//
// for simplify the RaftLog implement should manage all log entries
// that not truncated
type RaftLog struct {
	// storage contains all stable entries since the last snapshot.
	storage Storage

	// committed is the highest log position that is known to be in
	// stable storage on a quorum of nodes.
	committed uint64

	// applied is the highest log position that the application has
	// been instructed to apply to its state machine.
	// Invariant: applied <= committed
	applied uint64

	// log entries with index <= stabled are persisted to storage.
	// It is used to record the logs that are not persisted by storage yet.
	// Everytime handling `Ready`, the unstabled logs will be included.
	stabled uint64

	// all entries that have not yet compact.
	entries []pb.Entry

	// the incoming unstable snapshot, if any.
	// (Used in 2C)
	pendingSnapshot *pb.Snapshot
	// snapIndex 只在snapshot/截断时更新，可以理解为内存中 entries的firstIndex
	snapIndex uint64

	// Your Data Here (2A).
}

// newLog returns log using the given storage. It recovers the log
// to the state that it just commits and applies the latest snapshot.
func newLog(storage Storage) *RaftLog {
	// Your Code Here (2A).
	hs, _, err := storage.InitialState()
	if err != nil {
		panic(err)
	}
	firstIndex, err := storage.FirstIndex()
	if err != nil {
		// 如果没有已提交日志，那么commitindex初始化为0
		firstIndex = 1
	}
	lastindex, err := storage.LastIndex()
	if err != nil {
		lastindex = 0
	}
	entries, _ := storage.Entries(firstIndex, lastindex+1)
	return &RaftLog{
		storage:         storage,
		committed:       hs.Commit,
		applied:         firstIndex - 1,
		stabled:         lastindex,
		entries:         entries,
		pendingSnapshot: nil, // not used in 2A
		snapIndex:       firstIndex,
	}
}

// We need to compact the log entries in some point of time like
// storage compact stabled log entries prevent the log entries
// grow unlimitedly in memory
func (l *RaftLog) maybeCompact() {
	// Your Code Here (2C).
	index, _ := l.storage.FirstIndex()
	// 如果 first index > snapIndex,说明在这段时间又截断了某些日志，因此需要同步更新内存中的状态
	if index > l.snapIndex {
		//fmt.Println("----------", l.snapIndex, "----", index-l.snapIndex, len(l.entries))
		if len(l.entries) > 0 {
			// 酱紫更省空间
			entries := l.entries[index-l.snapIndex:]
			l.entries = make([]pb.Entry, len(entries))
			copy(l.entries, entries)
		}
		// update
		l.snapIndex = index
	}
}

// unstableEntries return all the unstable entries
func (l *RaftLog) unstableEntries() []pb.Entry {
	// Your Code Here (2A).
	if len(l.entries) > 0 {
		// 特判一下，防止越界
		if (l.stabled-l.FirstIndex()+1 < 0) || (l.stabled-l.FirstIndex()+1 > uint64(len(l.entries))) {
			return nil
		}
		return l.entries[l.stabled-l.FirstIndex()+1:]
	}
	return nil
}

// nextEnts returns all the committed but not applied entries
func (l *RaftLog) nextEnts() (ents []pb.Entry) {
	// Your Code Here (2A).
	// 特判一下有无log
	if len(l.entries) > 0 {
		// l.committed-l.FirstIndex()+1为这次要取的末尾，如果小于0就说明已经不在内存中了
		// l.applied - l.FirstIndex() + 1 是本次要取的起始index，如果大于 l.LastIndex()也说明不在内存中了
		if l.committed-l.FirstIndex()+1 < 0 || l.applied-l.FirstIndex()+1 > l.LastIndex() {
			return nil
		}
		// 特判防止越界
		if l.applied-l.FirstIndex()+1 >= 0 && l.committed-l.FirstIndex()+1 <= uint64(len(l.entries)) {
			return l.entries[l.applied-l.FirstIndex()+1 : l.committed-l.FirstIndex()+1]
		}
	}
	return nil
}

// FirstIndex 返回应该被操作的第一个索引值
// the first log entry that is available via Entries
func (l *RaftLog) FirstIndex() uint64 {
	if len(l.entries) == 0 {
		i, _ := l.storage.FirstIndex()
		return i - 1
	}
	return l.entries[0].Index
}

// LastIndex return the last index of the log entries
func (l *RaftLog) LastIndex() uint64 {
	// Your Code Here (2A).
	var snapIndex uint64 = 0
	if !IsEmptySnap(l.pendingSnapshot) {
		snapIndex = l.pendingSnapshot.Metadata.Index
	}
	if len(l.entries) == 0 {
		lastindex, _ := l.storage.LastIndex()
		return max(lastindex, snapIndex)
	}
	return max(l.entries[len(l.entries)-1].Index, snapIndex)
}

// 最后的entry的term
func (l *RaftLog) LastTerm() uint64 {
	term, _ := l.Term(l.LastIndex())
	return term
}

func (l *RaftLog) isUpToDate(index uint64, term uint64) bool {
	return term > l.LastTerm() || (term == l.LastTerm() && index >= l.LastIndex())
}

// Term return the term of the entry in the given index
// Term return the term of the entry in the given index
func (l *RaftLog) Term(i uint64) (uint64, error) {
	// Your Code Here (2A).
	// 有未persist的snapshot
	lastindex := l.LastIndex()
	if i > lastindex {
		return 0, fmt.Errorf("index out of range")
	}
	if i > l.stabled && len(l.entries) > 0 {
		if i >= l.entries[0].Index && (i-l.entries[0].Index) < uint64(len(l.entries)) {
			return l.entries[i-l.entries[0].Index].Term, nil
		}
	}
	term, err := l.storage.Term(i)
	// 这里需要判断一下，如果是被截断了，那么就要返回 errCompact
	if err == ErrUnavailable && !IsEmptySnap(l.pendingSnapshot) {
		if i == l.pendingSnapshot.Metadata.Index {
			return l.pendingSnapshot.Metadata.Term, nil
		} else {
			return 0, ErrCompacted
		}
	}
	return term, err
}

func (l *RaftLog) appliedTo(i uint64) {
	if i == 0 {
		return
	}
	if l.committed < i || i < l.applied {
		log.Fatal(fmt.Sprintf("applied(%d) is out of range [prevApplied(%d), committed(%d)]", i, l.applied, l.committed))
	}
	l.applied = i
}

// 获得相应区间的entries
func (l *RaftLog) findentries(lo uint64, hi uint64) []pb.Entry {
	var ents []pb.Entry

	// 如果有一部分在storage里面，先找那一部分
	if lo <= l.stabled {
		stable_ents, _ := l.storage.Entries(lo, min(hi, l.stabled+1))
		ents = append(ents, stable_ents...)
	}
	// 有未unstabled的部分
	if hi > l.stabled+1 {
		firstindex := l.entries[0].Index
		ents = append(ents, l.entries[max(l.stabled+1, lo)-firstindex:hi-firstindex]...)
	}
	//if flag == "copy" || flag == "all" {
	//	// DPrintf("log.go line 101 ents:%d", len(ents))
	//}
	return ents
}

// 加入新的entry
func (l *RaftLog) AppendEntries(ents ...*pb.Entry) {
	start := ents[0].Index
	l.stabled = min(l.stabled, start-1)
	// 如果当前的RaftLog.entries是空，或者非空但是start是刚好是下一个
	// 非空的话，和第一个比较
	if len(l.entries) == 0 {
		// 空的话什么都不做
	} else if start <= l.entries[0].Index {
		// 加入的ents在unstable entries之前，则前面的要推导重来
		l.entries = []pb.Entry{}
	} else if start > l.entries[0].Index {
		// 截掉ents之后的部分
		l.entries = l.entries[0 : start-l.entries[0].Index]
	}
	for _, ent := range ents {
		l.entries = append(l.entries, *ent)
	}
}

func (l *RaftLog) commitTo(commit uint64) {
	if l.committed < commit {
		if commit > l.LastIndex() {
			return
		}
		l.committed = commit
		//ToCPrint("[commitTo] commit to %v", commit)
	}
}

// 某个index之后，是否还有已经 commit 的 entries
func (l *RaftLog) hasEntriesSince(index uint64) bool {
	firstIndex, _ := l.storage.FirstIndex()
	offset := max(index+1, firstIndex)
	high := l.committed + 1
	//if flag == "copy" || flag == "all" {
	//	DPrintf("Node find entries_since from lo: %d to hi: %d", offset, high)
	//}
	return high > offset
}

// 返回某个index后的entries
func (l *RaftLog) entriesSince(index uint64) []pb.Entry {
	firstindex, _ := l.storage.FirstIndex()
	offset := max(index+1, firstindex)
	high := l.committed + 1
	if high > offset {
		//if flag == "copy" || flag == "all" {
		//	DPrintf("Node find entries_since from lo: %d to hi: %d", offset, high)
		//}
		return l.findentries(offset, high)
	}
	return []pb.Entry{}
}
