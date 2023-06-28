package goleveldb_test

import (
	"testing"

	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/db"
)

func BenchmarkCommit(b *testing.B) {
	db.Benchmark(db.Commit, opener, b)
}

func BenchmarkRead(b *testing.B) {
	db.Benchmark(db.Read, opener, b)
}

func BenchmarkReadCommit(b *testing.B) {
	db.Benchmark(db.ReadCommit, opener, b)
}
