package pebbledb

import (
	"runtime"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
)

const (
	PebbleDb = "pebbledb"
)

type pebbledb struct {
	db              *pebble.DB
	wo              *pebble.WriteOptions
	mu              sync.RWMutex
	batchWriterPool sync.Pool
	batchReaderPool sync.Pool
}

func Open(path string) (*pebbledb, error) {

	opts := &pebble.Options{}
	opts.Levels = []pebble.LevelOptions{{
		FilterPolicy: bloom.FilterPolicy(10),
	}}

	p := runtime.NumCPU() / 2
	opts.Experimental.MaxWriterConcurrency = p
	opts.MaxConcurrentCompactions = func() int {
		return p
	}

	opts.DisableWAL = true

	opts.EnsureDefaults()

	db, err := pebble.Open(path, opts)
	if err != nil {
		return nil, err
	}

	//bbto := grocksdb.NewDefaultBlockBasedTableOptions()
	//bbto.SetCacheIndexAndFilterBlocks(true)
	//bbto.SetCacheIndexAndFilterBlocksWithHighPriority(true)
	//
	//opts.SetBlockBasedTableFactory(bbto)
	//opts.OptimizeForPointLookup(1 * 1024) // using 1 gb of layer 0 block cache
	//opts.SetAllowConcurrentMemtableWrites(false)
	//opts.SetUseDirectIOForFlushAndCompaction(true)

	return &pebbledb{
		db: db,
		wo: &pebble.WriteOptions{Sync: false},
		batchWriterPool: sync.Pool{
			New: func() interface{} {
				return db.NewBatch()
			},
		},
	}, nil
}

func (r *pebbledb) Commit(keys [][]byte) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	batch := r.batchWriterPool.Get().(*pebble.Batch)
	defer func() {
		batch.Reset()
		r.batchWriterPool.Put(batch)
	}()

	for _, k := range keys {
		batch.Set(k, nil, nil)
	}
	return batch.Commit(r.wo)
}

func (r *pebbledb) DoNotExist(keys [][]byte) ([]bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]bool, len(keys))
	for i, key := range keys {
		_, _, err := r.db.Get(key)
		if err != nil && err != pebble.ErrNotFound {
			return nil, err
		}
		//closer.Close()
		result[i] = err == pebble.ErrNotFound
	}

	return result, nil
}

func (r *pebbledb) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.db.Close()
}
