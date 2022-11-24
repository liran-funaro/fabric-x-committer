package db

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

type DbTestInstance struct {
	rdb     Database
	cleanup func()
}

type DbOpener func(string) (Database, error)

const path = "dbpath"

func NewDbTestInstance(opener func(string) (Database, error)) *DbTestInstance {

	db, err := opener(path)
	if err != nil {
		panic(err)
	}
	if db == nil {
		panic("no db returned")
	}

	return &DbTestInstance{
		rdb: db,
		cleanup: func() {
			db.Close()
			os.RemoveAll(path)
		},
	}
}

func TestMultiGet(opener DbOpener, t *testing.T) {
	r := NewDbTestInstance(opener)
	defer r.cleanup()

	keys := [][]byte{
		[]byte("key1"),
		[]byte("key2"),
		[]byte("key3"),
	}
	doNotExist, err := r.rdb.DoNotExist(keys)
	require.NoError(t, err)
	require.Equal(t, []bool{true, true, true}, doNotExist)

	require.NoError(t, r.rdb.Commit([][]byte{[]byte("key3")}))

	doNotExist, err = r.rdb.DoNotExist(keys)
	require.NoError(t, err)
	require.Equal(t, []bool{true, true, false}, doNotExist)

	require.NoError(t, r.rdb.Commit([][]byte{[]byte("key2")}))

	doNotExist, err = r.rdb.DoNotExist(keys)
	require.NoError(t, err)
	require.Equal(t, []bool{true, false, false}, doNotExist)

	require.NoError(t, r.rdb.Commit([][]byte{[]byte("key1")}))

	doNotExist, err = r.rdb.DoNotExist(keys)
	require.NoError(t, err)
	require.Equal(t, []bool{false, false, false}, doNotExist)
}

func TestCommit(opener DbOpener, t *testing.T) {
	r := NewDbTestInstance(opener)
	defer r.cleanup()

	keys := [][]byte{[]byte("key1"), []byte("key2"), []byte("key3")}

	doNotExist, err := r.rdb.DoNotExist(keys)
	require.NoError(t, err)
	require.Equal(t, []bool{true, true, true}, doNotExist)

	require.NoError(t, r.rdb.Commit(keys))

	doNotExist, err = r.rdb.DoNotExist(keys)
	require.NoError(t, err)
	require.Equal(t, []bool{false, false, false}, doNotExist)
}

type Database interface {
	DoNotExist(keys [][]byte) ([]bool, error)
	Commit(keys [][]byte) error
	Close()
}
