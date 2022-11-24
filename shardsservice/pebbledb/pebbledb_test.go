package pebbledb_test

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/db"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/pebbledb"
	"testing"
)

var opener db.DbOpener = func(path string) (db.Database, error) {
	return pebbledb.Open(path)
}

func TestMultiGet(t *testing.T) {
	db.TestMultiGet(opener, t)
}

func TestCommit(t *testing.T) {
	db.TestCommit(opener, t)
}
