package rocksdb

import (
	"sync"

	"github.com/linxGnu/grocksdb"
)

type rocksdb struct {
	db *grocksdb.DB
	ro *grocksdb.ReadOptions
	wo *grocksdb.WriteOptions
	mu sync.RWMutex
}

func Open(path string) (*rocksdb, error) {
	bbto := grocksdb.NewDefaultBlockBasedTableOptions()
	bbto.SetCacheIndexAndFilterBlocks(true)
	bbto.SetCacheIndexAndFilterBlocksWithHighPriority(true)
	bbto.SetBlockCache(grocksdb.NewLRUCache(3 << 30))

	opts := grocksdb.NewDefaultOptions()
	opts.SetBlockBasedTableFactory(bbto)
	opts.SetCreateIfMissing(true)

	// system failure and recovery during open is not handled
	db, err := grocksdb.OpenDb(opts, path)
	if err != nil {
		return nil, err
	}

	ro := grocksdb.NewDefaultReadOptions()
	wo := grocksdb.NewDefaultWriteOptions()
	wo.SetSync(true)

	return &rocksdb{
		db: db,
		ro: ro,
		wo: wo,
	}, nil
}

func (r *rocksdb) Commit(keys [][]byte) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	batch := grocksdb.NewWriteBatch()

	for _, k := range keys {
		batch.Put(k, nil)
	}

	defer batch.Clear()
	return r.db.Write(r.wo, batch)
}

func (r *rocksdb) DoNotExist(keys [][]byte) ([]bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]bool, len(keys))

	values, err := r.db.MultiGet(r.ro, keys...)
	if err != nil {
		return result, err
	}

	for i, v := range values {
		result[i] = !v.Exists()
		go v.Free()
	}

	return result, nil
}

func (r *rocksdb) Close() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	r.db.Close()
}
