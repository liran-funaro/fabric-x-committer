package rocksdb

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

type rocksdbTestInstance struct {
	path    string
	rdb     *rocksdb
	cleanup func()
}

func NewRocksDBTestInstance(t *testing.T, path string) *rocksdbTestInstance {
	db, err := Open(path)
	require.NoError(t, err)
	require.NotNil(t, db)

	return &rocksdbTestInstance{
		path: path,
		rdb:  db,
		cleanup: func() {
			os.RemoveAll(path)
		},
	}
}

func TestMultiGet(t *testing.T) {
	r := NewRocksDBTestInstance(t, "TestMultiGet")
	defer r.cleanup()

	keys := [][]byte{
		[]byte("key1"),
		[]byte("key2"),
		[]byte("key3"),
	}
	doNotExist, err := r.rdb.DoNotExist(keys)
	require.NoError(t, err)
	require.Equal(t, []bool{true, true, true}, doNotExist)

	require.NoError(t, r.rdb.db.Put(r.rdb.wo, []byte("key3"), nil))

	doNotExist, err = r.rdb.DoNotExist(keys)
	require.NoError(t, err)
	require.Equal(t, []bool{true, true, false}, doNotExist)

	require.NoError(t, r.rdb.db.Put(r.rdb.wo, []byte("key2"), nil))

	doNotExist, err = r.rdb.DoNotExist(keys)
	require.NoError(t, err)
	require.Equal(t, []bool{true, false, false}, doNotExist)

	require.NoError(t, r.rdb.db.Put(r.rdb.wo, []byte("key1"), nil))

	doNotExist, err = r.rdb.DoNotExist(keys)
	require.NoError(t, err)
	require.Equal(t, []bool{false, false, false}, doNotExist)
}

func TestCommit(t *testing.T) {
	r := NewRocksDBTestInstance(t, "TestCommit")
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
