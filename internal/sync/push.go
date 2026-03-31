package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/datapointchris/todoui/internal/db/generated"
)

// executePush translates a pending sync operation to an HTTP request.
// Returns nil on success, or on server rejection (404/409) which means
// the operation should be dropped (server wins).
// Returns non-nil error on network/transient failures that should be retried.
func (e *Engine) executePush(op generated.PendingSync) error {
	opType := OpType(op.Operation)

	switch opType {
	case OpCreateProject:
		return e.pushJSON(http.MethodPost, "/projects/", op.Payload)
	case OpUpdateProject:
		return e.pushJSON(http.MethodPatch, fmt.Sprintf("/projects/%s/", op.EntityID), op.Payload)
	case OpDeleteProject:
		return e.pushDelete(fmt.Sprintf("/projects/%s/", op.EntityID))

	case OpCreateItem:
		return e.pushJSON(http.MethodPost, "/project-items/", op.Payload)
	case OpUpdateItem:
		return e.pushJSON(http.MethodPatch, fmt.Sprintf("/project-items/%s/", op.EntityID), op.Payload)
	case OpDeleteItem:
		return e.pushDelete(fmt.Sprintf("/project-items/%s/", op.EntityID))

	case OpReorderItem:
		return e.pushJSON(http.MethodPatch, fmt.Sprintf("/project-items/%s/reorder", op.EntityID), op.Payload)

	case OpAddToProject:
		return e.pushJSON(http.MethodPost, fmt.Sprintf("/project-items/%s/projects", op.EntityID), op.Payload)
	case OpRemoveFromProject:
		var p struct {
			ProjectID string `json:"project_id"`
		}
		_ = json.Unmarshal([]byte(op.Payload), &p)
		return e.pushDelete(fmt.Sprintf("/project-items/%s/projects/%s", op.EntityID, p.ProjectID))

	case OpCreateTask:
		var p struct {
			ItemID string `json:"item_id"`
		}
		_ = json.Unmarshal([]byte(op.Payload), &p)
		return e.pushJSON(http.MethodPost, fmt.Sprintf("/project-items/%s/tasks/", p.ItemID), op.Payload)
	case OpUpdateTask:
		var p struct {
			ItemID string `json:"item_id"`
		}
		_ = json.Unmarshal([]byte(op.Payload), &p)
		return e.pushJSON(http.MethodPatch, fmt.Sprintf("/project-items/%s/tasks/%s/", p.ItemID, op.EntityID), op.Payload)
	case OpDeleteTask:
		var p struct {
			ItemID string `json:"item_id"`
		}
		_ = json.Unmarshal([]byte(op.Payload), &p)
		return e.pushDelete(fmt.Sprintf("/project-items/%s/tasks/%s/", p.ItemID, op.EntityID))
	case OpCompleteTask:
		var p struct {
			ItemID string `json:"item_id"`
		}
		_ = json.Unmarshal([]byte(op.Payload), &p)
		return e.pushJSON(http.MethodPost, fmt.Sprintf("/project-items/%s/tasks/%s/complete/", p.ItemID, op.EntityID), "")

	case OpAddDependency:
		return e.pushJSON(http.MethodPost, fmt.Sprintf("/project-items/%s/dependencies", op.EntityID), op.Payload)
	case OpRemoveDependency:
		var p struct {
			DependsOnID string `json:"depends_on_id"`
		}
		_ = json.Unmarshal([]byte(op.Payload), &p)
		return e.pushDelete(fmt.Sprintf("/project-items/%s/dependencies/%s", op.EntityID, p.DependsOnID))

	default:
		return fmt.Errorf("unknown sync operation: %s", op.Operation)
	}
}

// pushJSON sends a JSON request and handles the response.
// 404/409 are treated as "drop this op" (returns nil).
// Network errors and 5xx are retryable (returns error).
func (e *Engine) pushJSON(method, path string, payload string) error {
	var body io.Reader
	if payload != "" {
		body = bytes.NewReader([]byte(payload))
	}

	req, err := http.NewRequestWithContext(e.ctx, method, e.apiURL+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	return e.classifyResponse(resp.StatusCode)
}

// pushDelete sends a DELETE request.
func (e *Engine) pushDelete(path string) error {
	req, err := http.NewRequestWithContext(e.ctx, http.MethodDelete, e.apiURL+path, nil)
	if err != nil {
		return err
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	return e.classifyResponse(resp.StatusCode)
}

// classifyResponse decides whether a status code means success, drop, or retry.
func (e *Engine) classifyResponse(status int) error {
	switch {
	case status < 400:
		return nil // success
	case status == http.StatusNotFound, status == http.StatusConflict:
		return nil // drop: server wins, next pull reconciles
	case status >= 500:
		return fmt.Errorf("server error: %d", status)
	default:
		return fmt.Errorf("push rejected: %d", status)
	}
}

// friendlyNetErr translates raw network errors into user-readable messages.
func friendlyNetErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("cannot reach API server (connection refused)")
	case strings.Contains(msg, "no such host"):
		return fmt.Errorf("cannot reach API server (DNS lookup failed)")
	case strings.Contains(msg, "Timeout") || strings.Contains(msg, "deadline exceeded"):
		return fmt.Errorf("API server did not respond (timeout)")
	default:
		return fmt.Errorf("network error: %w", err)
	}
}
