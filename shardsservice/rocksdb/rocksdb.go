package rocksdb

import (
	"runtime"
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
	// bbto.SetBlockCache(grocksdb.NewLRUCache(1 << 30))
	bbto.SetFilterPolicy(grocksdb.NewBloomFilterFull(10))

	opts := grocksdb.NewDefaultOptions()
	opts.SetBlockBasedTableFactory(bbto)
	opts.SetCreateIfMissing(true)

	p := runtime.NumCPU() / 2
	opts.IncreaseParallelism(p) // half of our cores allocated to the db
	opts.SetMaxBackgroundJobs(p)

	opts.OptimizeForPointLookup(1 * 1024) // using 1 gb of layer 0 block cache
	opts.SetAllowConcurrentMemtableWrites(false)
	// opts.SetUseDirectReads(true)
	opts.SetUseDirectIOForFlushAndCompaction(true)

	// system failure and recovery during open is not handled
	db, err := grocksdb.OpenDb(opts, path)
	if err != nil {
		return nil, err
	}

	ro := grocksdb.NewDefaultReadOptions()
	wo := grocksdb.NewDefaultWriteOptions()
	wo.DisableWAL(true)
	wo.SetSync(false)

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

	defer batch.Destroy()
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
		v.Free()
	}

	return result, nil
}

func (r *rocksdb) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.db.Close()
}
