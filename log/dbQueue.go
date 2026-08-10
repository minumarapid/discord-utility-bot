package msglog

import (
	"log"
	"time"

	"gorm.io/gorm"
)

type MsgCacheJob[T any] struct {
	Data T
}

type DBCacheQueue[T any] struct {
	ch chan T
	onFlush func([]T)
}

func NewDBCacheQueue[T any](db *gorm.DB, bufferSize int, batchSize int, onFlush func([]T)) *DBCacheQueue[T] {
	q := &DBCacheQueue[T]{
		ch: make(chan T, bufferSize),
		onFlush: onFlush,
	}

	go func() {
		var buffer []T
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		flush := func() {
			if len(buffer) == 0 {
				return
			}
			if err := db.Create(&buffer).Error; err != nil {
				log.Println("Failed to bulk insert cache:", err)
			} else {
				q.onFlush(buffer)
				buffer = buffer[:0]
			}
		}

		for {
			select {
			case item, ok := <-q.ch:
				if !ok {
					flush()
					return
				}
				buffer = append(buffer, item)
				if len(buffer) >= batchSize {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	}()

	return q
}

func (q *DBCacheQueue[T]) Push(item T) {
	select {
	case q.ch <- item:
	default:
		log.Println("Cache queue full, dropping item")
	}
}
