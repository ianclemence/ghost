package agent

import (
	"sync"

	"github.com/ianclemence/ghost/pkg/doctor"
)

// retrievalStat aggregates observed retrieval latencies for one retrieval
// path. These are measured where the work happens (RAG context assembly,
// memory-note search) and surfaced in Doctor — the first step toward
// cost-aware retrieval scoring (Phase 3). Counts only; no query content,
// no payloads.
type retrievalStat struct {
	Queries int64
	TotalMs int64
	LastMs  int64
}

func (s *retrievalStat) avg() float64 {
	if s.Queries == 0 {
		return 0
	}
	return float64(s.TotalMs) / float64(s.Queries)
}

// retrievalRecorder keeps per-path latency aggregates. Zero value is ready;
// all methods are safe for concurrent turn execution.
type retrievalRecorder struct {
	mu    sync.Mutex
	paths map[string]*retrievalStat
}

// observe records one retrieval call of path ("rag", "memo") in milliseconds.
func (r *retrievalRecorder) observe(path string, ms int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.paths == nil {
		r.paths = map[string]*retrievalStat{}
	}
	st, ok := r.paths[path]
	if !ok {
		st = &retrievalStat{}
		r.paths[path] = st
	}
	st.Queries++
	st.TotalMs += ms
	st.LastMs = ms
}

// retrievalSnapshot converts observed aggregates into the Doctor-facing
// shape. Safe to call at any time; unknown paths report zero.
func (r *retrievalRecorder) snapshot() doctor.RetrievalStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	path := func(name string) doctor.RetrievalPath {
		st := r.paths[name]
		if st == nil {
			return doctor.RetrievalPath{}
		}
		return doctor.RetrievalPath{Queries: st.Queries, AvgMs: st.avg(), LastMs: st.LastMs}
	}
	return doctor.RetrievalStats{RAG: path("rag"), Memo: path("memo")}
}
