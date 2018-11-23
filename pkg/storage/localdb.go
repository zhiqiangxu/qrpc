package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"time"

	"github.com/dgraph-io/badger"
	"github.com/zhiqiangxu/qrpc"
)

// LocalDB models local db
type LocalDB struct {
	*badger.DB

	mu          sync.Mutex
	seqMap      map[string]*badger.Sequence
	processMap  map[int]*processor
	processorID int
}

type processor struct {
	cancelFunc context.CancelFunc
	wg         *sync.WaitGroup
}

// cancel process and wait for it to finish
func (p *processor) Cancel() {
	p.cancelFunc()
	p.wg.Wait()
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
	// KVPrefix for plain kv
	KVPrefix = "kv:"
)

// KeyPrefixForTable returns key prefix for table
func KeyPrefixForTable(table string) []byte {
	return join(TablePrefix, table)
}

// KeyForKV returns key for plain kv
func KeyForKV(key ...string) []byte {
	return join(append([]string{KVPrefix}, key...)...)
}

func join(s ...string) []byte {
	var buf bytes.Buffer
	for _, v := range s {
		buf.WriteString(v)
	}
	return buf.Bytes()
}

// methods for table
// CAUTION: table names MUST NEVER be substring of each other!!

// InsertKeyForTable generate key for insert
func (l *LocalDB) InsertKeyForTable(table string) ([]byte, uint64, error) {
	seq, err := l.SeqForTable(table)
	if err != nil {
		return nil, 0, err
	}

	id, err := seq.Next()
	if err != nil {
		return nil, 0, err
	}
	return l.TableKeyForID(table, id), id, nil
}

// TableKeyForID returns underlying key for table/id pair
func (l *LocalDB) TableKeyForID(table string, id uint64) []byte {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, id)

	prefix := KeyPrefixForTable(table)

	key := append(prefix, bytes...)

	return key
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
func (l *LocalDB) InsertTable(table string, value []byte) (uint64, error) {
	key, id, err := l.InsertKeyForTable(table)
	if err != nil {
		return 0, err
	}

	err = l.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value)
	})

	return id, err
}

// QueryByIDFromTable get the value for table/id pair
func (l *LocalDB) QueryByIDFromTable(table string, id uint64, f func([]byte) error) error {

	return l.View(func(txn *badger.Txn) error {
		key := l.TableKeyForID(table, id)
		item, err := txn.Get(key)
		err = item.Value(func(val []byte) error {
			return f(val)
		})
		return err
	})
}

// IterateTable rows in table in insert order
// stops on first error
func (l *LocalDB) IterateTable(table string, f func([]byte, []byte) error) error {
	return l.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := KeyPrefixForTable(table)
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

// RegisterProcessForTable register a func to be called on table rows periodically
func (l *LocalDB) RegisterProcessForTable(table string, interval time.Duration, f func([]byte, []byte) error) int {

	ctx, cancelFunc := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	qrpc.GoFunc(&wg, func() {
		for {
			select {
			case <-time.After(interval):
				l.IterateTable(table, func(k []byte, v []byte) error {
					err := f(k, v)
					if err != nil {
						if qrpc.Logger != nil {
							qrpc.Logger.Error("RegisterProcessForTable f", err)
						}
						return nil // nonstop
					}

					err = l.Update(func(txn *badger.Txn) error {
						return txn.Delete(k)
					})
					if err != nil {
						if qrpc.Logger != nil {
							qrpc.Logger.Error("RegisterProcessForTable Delete", err)
						}
					}

					return nil // nonstop
				})
			case <-ctx.Done():
				break
			}
		}
	})

	processor := &processor{wg: &wg, cancelFunc: cancelFunc}

	l.mu.Lock()
	processorID := l.processorID
	l.processorID++
	l.processMap[processorID] = processor
	l.mu.Unlock()

	return processorID
}

// UnregProcessForTable cancels a processor by processID
func (l *LocalDB) UnregProcessForTable(processID int) {
	l.mu.Lock()
	processor, ok := l.processMap[processID]
	if ok {
		delete(l.processMap, processID)
	}
	l.mu.Unlock()

	if ok {
		processor.Cancel()
	}
}
