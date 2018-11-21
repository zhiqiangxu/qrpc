package storage

import "github.com/dgraph-io/badger"

// OpenLocalDB for db per node
func OpenLocalDB(opts badger.Options) (*badger.DB, error) {
	return badger.Open(opts)
}
