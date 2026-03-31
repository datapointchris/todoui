package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	gosync "sync"
	"time"

	"github.com/datapointchris/todoui/internal/db/generated"
)

// Engine orchestrates push and pull synchronization with the remote API.
type Engine struct {
	db     *sql.DB
	q      *generated.Queries
	client *http.Client
	apiURL string

	mu     gosync.Mutex
	status SyncStatus

	pushCh chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a sync engine. Call Start() to launch the background push loop.
func New(db *sql.DB, apiURL string) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		db:     db,
		q:      generated.New(db),
		client: &http.Client{Timeout: 10 * time.Second},
		apiURL: apiURL,
		pushCh: make(chan struct{}, 1),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start launches the background push loop goroutine.
func (e *Engine) Start() {
	go e.pushLoop()
}

// Stop shuts down the background goroutine gracefully.
func (e *Engine) Stop() {
	e.cancel()
}

// QueueOp inserts a pending sync operation into the database.
func (e *Engine) QueueOp(op OpType, entityID string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling sync payload: %w", err)
	}
	return e.q.InsertPendingSync(e.ctx, generated.InsertPendingSyncParams{
		Operation:  string(op),
		EntityType: op.entityType(),
		EntityID:   entityID,
		Payload:    string(data),
	})
}

// Notify signals the push loop to wake up and process pending operations.
// Non-blocking: if the push loop is already signaled, this is a no-op.
func (e *Engine) Notify() {
	select {
	case e.pushCh <- struct{}{}:
	default:
	}
}

// Status returns the current sync status (thread-safe).
func (e *Engine) Status() SyncStatus {
	e.mu.Lock()
	defer e.mu.Unlock()

	count, err := e.q.CountPendingSync(e.ctx)
	if err != nil {
		return e.status
	}
	e.status.PendingCount = int(count)
	return e.status
}

func (e *Engine) setStatus(fn func(s *SyncStatus)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fn(&e.status)
}

func (e *Engine) pushLoop() {
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-e.pushCh:
			e.drainPendingOps()
		}
	}
}

func (e *Engine) drainPendingOps() {
	e.setStatus(func(s *SyncStatus) { s.Syncing = true })
	defer e.setStatus(func(s *SyncStatus) { s.Syncing = false })

	for {
		if e.ctx.Err() != nil {
			return
		}

		op, err := e.q.GetOldestPendingSync(e.ctx)
		if err == sql.ErrNoRows {
			e.setStatus(func(s *SyncStatus) {
				s.Connected = true
				s.LastError = ""
			})
			return
		}
		if err != nil {
			log.Printf("sync: reading pending op: %v", err)
			return
		}

		if err := e.executePush(op); err != nil {
			e.setStatus(func(s *SyncStatus) {
				s.Connected = false
				s.LastError = err.Error()
			})
			_ = e.q.UpdatePendingSyncError(e.ctx, generated.UpdatePendingSyncErrorParams{
				LastError: sql.NullString{String: err.Error(), Valid: true},
				ID:        op.ID,
			})
			// Back off briefly, then exit loop. Will retry on next Notify.
			select {
			case <-time.After(2 * time.Second):
			case <-e.ctx.Done():
			}
			return
		}

		_ = e.q.DeletePendingSync(e.ctx, op.ID)
		e.setStatus(func(s *SyncStatus) {
			s.Connected = true
			s.LastError = ""
		})
	}
}
