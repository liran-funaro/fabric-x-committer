package pebbledb_test

import (
	"testing"

	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/db"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/db/pebbledb"
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
