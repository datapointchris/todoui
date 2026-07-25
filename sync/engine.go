package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	gosync "sync"
	"time"

	"github.com/datapointchris/todoui/db/generated"
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
	done   chan struct{}
}

// New creates a sync engine. Call Start() to launch the background push loop.
// If apiKey is non-empty, it is sent as a Bearer token on every request.
func New(db *sql.DB, apiURL, apiKey string) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Timeout: 10 * time.Second}
	if apiKey != "" {
		client.Transport = &authTransport{
			key:  apiKey,
			base: http.DefaultTransport,
		}
	}
	return &Engine{
		db:     db,
		q:      generated.New(db),
		client: client,
		apiURL: apiURL,
		pushCh: make(chan struct{}, 1),
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

// APIURL returns the remote API base URL this engine syncs with.
func (e *Engine) APIURL() string { return e.apiURL }

// authTransport injects an Authorization header into every outgoing request.
type authTransport struct {
	key  string
	base http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.key)
	return t.base.RoundTrip(req)
}

// Start launches the background push loop goroutine.
func (e *Engine) Start() {
	go func() {
		defer close(e.done)
		e.pushLoop()
	}()
}

// Stop cancels the background loop and waits for it to finish. Callers close the
// database right after this returns, so returning early leaves the loop mid-query
// against a closed handle — which is where "sql: database is closed" came from on
// every CLI invocation. The timeout keeps an in-flight HTTP push from hanging exit.
func (e *Engine) Stop() {
	e.cancel()
	select {
	case <-e.done:
	case <-time.After(2 * time.Second):
	}
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

// DropOpsForEntity removes every queued operation for one entity and reports how
// many were dropped. Used when a local change is reversed: the queued operations
// describe a state that no longer exists, and pushing them would recreate it on
// the server. A non-zero count also means nothing for that entity reached the
// server yet, since operations are deleted once pushed.
func (e *Engine) DropOpsForEntity(entityID string) (int64, error) {
	return e.q.DeletePendingSyncByEntity(e.ctx, entityID)
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
			// Shutdown cancels the context mid-query. That is the normal exit
			// path, not a fault worth printing on every invocation.
			if !errors.Is(err, context.Canceled) {
				log.Printf("sync: reading pending op: %v", err)
			}
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
