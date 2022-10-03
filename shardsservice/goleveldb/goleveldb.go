package goleveldb

import (
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

type goleveldb struct {
	db *leveldb.DB
	mu sync.RWMutex
}

func Open(path string) (*goleveldb, error) {
	o := &opt.Options{
		Filter:             filter.NewBloomFilter(10),
		BlockCacheCapacity: 32 * opt.MiB,
	}
	db, err := leveldb.OpenFile(path, o)
	if err != nil {
		return nil, err
	}

	return &goleveldb{
		db: db,
	}, nil
}

func (l *goleveldb) Commit(keys [][]byte) error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	batch := &leveldb.Batch{}

	for _, k := range keys {
		batch.Put(k, nil)
	}

	defer batch.Reset()
	return l.db.Write(batch, &opt.WriteOptions{Sync: true})
}

func (l *goleveldb) DoNotExist(keys [][]byte) ([]bool, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]bool, len(keys))

	for i, k := range keys {
		exist, err := l.db.Has(k, &opt.ReadOptions{})
		if err != nil {
			return nil, err
		}
		result[i] = !exist
	}

	return result, nil
}

func (l *goleveldb) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.db.Close()
}
