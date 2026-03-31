package sync

// SyncStatus represents the current state of the sync engine for TUI display.
type SyncStatus struct {
	Connected    bool
	PendingCount int
	LastError    string
	Syncing      bool
}
