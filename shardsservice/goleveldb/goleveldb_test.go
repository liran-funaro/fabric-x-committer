package goleveldb_test

import (
	"testing"

	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/db"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/goleveldb"
)

var opener db.DbOpener = func(path string) (db.Database, error) {
	return goleveldb.Open(path)
}

func TestMultiGet(t *testing.T) {
	db.TestMultiGet(opener, t)
}

func TestCommit(t *testing.T) {
	db.TestCommit(opener, t)
}
