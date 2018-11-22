package storage

import (
	"bytes"
	"encoding/binary"
	"sync"

	"github.com/dgraph-io/badger"
	"github.com/zhiqiangxu/qrpc"
)

// LocalDB models local db
type LocalDB struct {
	*badger.DB

	mu     sync.Mutex
	seqMap map[string]*badger.Sequence
}

// OpenLocalDB for db per node
func OpenLocalDB(opts badger.Options) (*LocalDB, error) {
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}
	return &LocalDB{DB: db, seqMap: make(map[string]*badger.Sequence)}, nil
}

// Request for serialization
type Request struct {
	Flags   qrpc.FrameFlag
	Cmd     qrpc.Cmd
	Payload []byte
}

const (
	// TablePrefix is for table
	TablePrefix = "tbl:"
)

// KeyPrefixForTable returns key prefix for table
func (l *LocalDB) KeyPrefixForTable(table string) []byte {
	var buf bytes.Buffer
	buf.WriteString(TablePrefix)
	buf.WriteString(table)
	return buf.Bytes()
}

// InsertKeyForTable generate key for insert
func (l *LocalDB) InsertKeyForTable(table string) ([]byte, error) {
	seq, err := l.SeqForTable(table)
	if err != nil {
		return nil, err
	}

	id, err := seq.Next()
	if err != nil {
		return nil, err
	}
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, id)

	prefix := l.KeyPrefixForTable(table)

	key := append(prefix, bytes...)
	return key, nil
}

// SeqForTable returns sequence for table
func (l *LocalDB) SeqForTable(table string) (*badger.Sequence, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	seq, ok := l.seqMap[table]
	if ok {
		return seq, nil
	}

	seq, err := l.GetSequence(qrpc.Slice(table), 1000)

	if err != nil {
		return nil, err
	}

	l.seqMap[table] = seq

	return seq, nil

}

// InsertTable insert a new row into table
func (l *LocalDB) InsertTable(table string, value []byte) error {
	key, err := l.InsertKeyForTable(table)
	if err != nil {
		return err
	}

	err = l.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value)
	})

	return err
}

// Iterate rows in table in insert order
func (l *LocalDB) Iterate(table string, f func([]byte, []byte) error) error {
	return l.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := l.KeyPrefixForTable(table)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			k := item.Key()
			err := item.Value(func(v []byte) error {
				return f(k, v)
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}
