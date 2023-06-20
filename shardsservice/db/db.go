package db

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

type Database interface {
	DoNotExist(keys [][]byte) ([]bool, error)
	Commit(keys [][]byte) error
	Close()
}

var once sync.Once
var factories map[string]Factory

type Factory func(path string) (Database, error)

func Register(name string, f Factory) {
	once.Do(func() {
		factories = make(map[string]Factory)
	})

	if _, alreadyExists := factories[name]; alreadyExists {
		panic(fmt.Errorf("cannot register db `%s`! already exists", name))
	}

	factories[name] = f
}

func RegisteredDB() []string {
	var registeredDBs []string
	for k, _ := range factories {
		registeredDBs = append(registeredDBs, k)
	}

	return registeredDBs
}

func OpenDb(dbType string, path string) (Database, error) {
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}

	if f, ok := factories[strings.ToLower(dbType)]; ok {
		return f(path)
	}

	fmt.Printf("f: %v\n", factories)

	return nil, fmt.Errorf("unknown db type `%s`", dbType)
}
