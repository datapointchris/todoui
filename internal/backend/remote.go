package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/datapointchris/todoui/internal/model"
)

// RemoteBackend connects to a todoui API server over HTTP.
type RemoteBackend struct {
	client *http.Client
	apiURL string // base URL without trailing slash
}

// NewRemoteBackend creates a backend that communicates with a remote API server.
func NewRemoteBackend(apiURL string) *RemoteBackend {
	return &RemoteBackend{
		client: &http.Client{Timeout: 10 * time.Second},
		apiURL: strings.TrimRight(apiURL, "/"),
	}
}

// --- HTTP helpers ---

func (r *RemoteBackend) get(path string, result any) error {
	resp, err := r.client.Get(r.apiURL + path)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	return r.handleResponse(resp, result)
}

func (r *RemoteBackend) post(path string, body any, result any) error {
	return r.doJSON(http.MethodPost, path, body, result)
}

func (r *RemoteBackend) patch(path string, body any, result any) error {
	return r.doJSON(http.MethodPatch, path, body, result)
}

func (r *RemoteBackend) delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, r.apiURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return r.parseError(resp)
	}
	return nil
}

func (r *RemoteBackend) doJSON(method string, path string, body any, result any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, r.apiURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	return r.handleResponse(resp, result)
}

func (r *RemoteBackend) handleResponse(resp *http.Response, result any) error {
	if resp.StatusCode >= 400 {
		return r.parseError(resp)
	}
	if result == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

// parseError maps HTTP error responses from ichrisbirch FastAPI to domain errors.
// FastAPI returns errors as {"detail": "message"} and uses 409 Conflict for
// cyclic dependencies and last-project violations (not 400 Bad Request).
func (r *RemoteBackend) parseError(resp *http.Response) error {
	var apiErr struct {
		Detail string `json:"detail"`
	}
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &apiErr)

	msg := apiErr.Detail
	if msg == "" {
		msg = string(body)
	}

	switch resp.StatusCode {
	case http.StatusNotFound:
		return model.ErrNotFound
	case http.StatusConflict:
		switch {
		case strings.Contains(msg, "cyclic"):
			return model.ErrCyclicDependency
		case strings.Contains(msg, "at least one"):
			return model.ErrLastProject
		default:
			return model.ErrDuplicateName
		}
	case http.StatusBadRequest:
		if strings.Contains(msg, "nothing to undo") {
			return model.ErrNothingToUndo
		}
		return fmt.Errorf("bad request: %s", msg)
	default:
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, msg)
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
	case strings.Contains(msg, "Timeout"):
		return fmt.Errorf("API server did not respond (timeout)")
	case strings.Contains(msg, "deadline exceeded"):
		return fmt.Errorf("API server did not respond (timeout)")
	default:
		return fmt.Errorf("network error: %w", err)
	}
}

// --- Projects ---

func (r *RemoteBackend) ListProjects() ([]model.ProjectWithItemCount, error) {
	var projects []model.ProjectWithItemCount
	err := r.get("/projects/", &projects)
	return projects, err
}

func (r *RemoteBackend) GetProject(id int64) (*model.ProjectWithItemCount, error) {
	var project model.ProjectWithItemCount
	err := r.get(fmt.Sprintf("/projects/%d/", id), &project)
	return &project, err
}

func (r *RemoteBackend) CreateProject(name string) (*model.Project, error) {
	var project model.Project
	err := r.post("/projects/", map[string]string{"name": name}, &project)
	return &project, err
}

func (r *RemoteBackend) UpdateProject(id int64, input model.UpdateProject) (*model.Project, error) {
	var project model.Project
	err := r.patch(fmt.Sprintf("/projects/%d/", id), input, &project)
	return &project, err
}

func (r *RemoteBackend) DeleteProject(id int64) error {
	return r.delete(fmt.Sprintf("/projects/%d/", id))
}

// --- Items ---

func (r *RemoteBackend) ListAllItems() ([]model.ProjectItem, error) {
	var items []model.ProjectItem
	err := r.get("/project-items/", &items)
	return items, err
}

func (r *RemoteBackend) ListItemsByProject(projectID int64) ([]model.ProjectItemInProject, error) {
	var items []model.ProjectItemInProject
	err := r.get(fmt.Sprintf("/projects/%d/items", projectID), &items)
	return items, err
}

func (r *RemoteBackend) GetItem(id int64) (*model.ProjectItemDetail, error) {
	var detail model.ProjectItemDetail
	err := r.get(fmt.Sprintf("/project-items/%d/", id), &detail)
	return &detail, err
}

func (r *RemoteBackend) CreateItem(input model.CreateProjectItem) (*model.ProjectItemDetail, error) {
	var detail model.ProjectItemDetail
	err := r.post("/project-items/", input, &detail)
	return &detail, err
}

func (r *RemoteBackend) UpdateItem(id int64, input model.UpdateProjectItem) (*model.ProjectItem, error) {
	var item model.ProjectItem
	err := r.patch(fmt.Sprintf("/project-items/%d/", id), input, &item)
	return &item, err
}

func (r *RemoteBackend) DeleteItem(id int64) error {
	return r.delete(fmt.Sprintf("/project-items/%d/", id))
}

func (r *RemoteBackend) ReorderItem(itemID int64, projectID int64, newPosition int) error {
	body := struct {
		ProjectID int64 `json:"project_id"`
		Position  int   `json:"position"`
	}{ProjectID: projectID, Position: newPosition}
	return r.patch(fmt.Sprintf("/project-items/%d/reorder", itemID), body, nil)
}

// --- Multi-project membership ---

func (r *RemoteBackend) AddToProject(itemID int64, projectID int64) error {
	body := struct {
		ProjectID int64 `json:"project_id"`
	}{ProjectID: projectID}
	return r.post(fmt.Sprintf("/project-items/%d/projects", itemID), body, nil)
}

func (r *RemoteBackend) RemoveFromProject(itemID int64, projectID int64) error {
	return r.delete(fmt.Sprintf("/project-items/%d/projects/%d", itemID, projectID))
}

func (r *RemoteBackend) GetItemProjects(itemID int64) ([]model.Project, error) {
	var projects []model.Project
	err := r.get(fmt.Sprintf("/project-items/%d/projects", itemID), &projects)
	return projects, err
}

// --- Dependencies ---

func (r *RemoteBackend) AddDependency(itemID int64, dependsOn int64) error {
	body := struct {
		DependsOnID int64 `json:"depends_on_id"`
	}{DependsOnID: dependsOn}
	return r.post(fmt.Sprintf("/project-items/%d/dependencies", itemID), body, nil)
}

func (r *RemoteBackend) RemoveDependency(itemID int64, dependsOn int64) error {
	return r.delete(fmt.Sprintf("/project-items/%d/dependencies/%d", itemID, dependsOn))
}

func (r *RemoteBackend) GetBlockers(itemID int64) ([]model.ProjectItem, error) {
	var blockers []model.ProjectItem
	err := r.get(fmt.Sprintf("/project-items/%d/blockers", itemID), &blockers)
	return blockers, err
}

// --- Search ---

func (r *RemoteBackend) Search(query string) ([]model.ProjectItem, error) {
	var results []model.ProjectItem
	err := r.get("/project-items/search?q="+url.QueryEscape(query), &results)
	return results, err
}

// --- Filters ---

func (r *RemoteBackend) ListBlocked() ([]model.ProjectItem, error) {
	var items []model.ProjectItem
	err := r.get("/project-items/blocked", &items)
	return items, err
}

func (r *RemoteBackend) ListArchived(projectID int64) ([]model.ProjectItemInProject, error) {
	var items []model.ProjectItemInProject
	err := r.get(fmt.Sprintf("/projects/%d/items?archived=true", projectID), &items)
	return items, err
}

// --- Undo ---

func (r *RemoteBackend) Undo() (string, error) {
	var result struct {
		Description string `json:"description"`
	}
	err := r.post("/undo/", nil, &result)
	return result.Description, err
}

func (r *RemoteBackend) CanUndo() (bool, error) {
	var result struct {
		CanUndo bool `json:"can_undo"`
	}
	err := r.get("/undo/", &result)
	return result.CanUndo, err
}
