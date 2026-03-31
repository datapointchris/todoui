package sync

import (
	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/model"
)

// SyncBackend wraps LocalBackend and queues sync operations after mutations.
// Read methods delegate directly; write methods delegate then queue + notify.
type SyncBackend struct {
	local  *backend.LocalBackend
	engine *Engine
}

// Compile-time check that SyncBackend implements Backend.
var _ backend.Backend = (*SyncBackend)(nil)

// NewSyncBackend creates a backend that writes locally and syncs to the remote API.
func NewSyncBackend(local *backend.LocalBackend, engine *Engine) *SyncBackend {
	return &SyncBackend{local: local, engine: engine}
}

// --- Read methods: pass through to local ---

func (s *SyncBackend) ListProjects() ([]model.ProjectWithItemCount, error) {
	return s.local.ListProjects()
}

func (s *SyncBackend) GetProject(id string) (*model.ProjectWithItemCount, error) {
	return s.local.GetProject(id)
}

func (s *SyncBackend) ListAllItems() ([]model.ProjectItem, error) {
	return s.local.ListAllItems()
}

func (s *SyncBackend) ListItemsByProject(projectID string) ([]model.ProjectItemInProject, error) {
	return s.local.ListItemsByProject(projectID)
}

func (s *SyncBackend) GetItem(id string) (*model.ProjectItemDetail, error) {
	return s.local.GetItem(id)
}

func (s *SyncBackend) GetItemProjects(itemID string) ([]model.Project, error) {
	return s.local.GetItemProjects(itemID)
}

func (s *SyncBackend) GetBlockers(itemID string) ([]model.ProjectItem, error) {
	return s.local.GetBlockers(itemID)
}

func (s *SyncBackend) ListTasks(itemID string) ([]model.ProjectItemTask, error) {
	return s.local.ListTasks(itemID)
}

func (s *SyncBackend) Search(query string) ([]model.ProjectItem, error) {
	return s.local.Search(query)
}

func (s *SyncBackend) ListBlocked() ([]model.ProjectItem, error) {
	return s.local.ListBlocked()
}

func (s *SyncBackend) ListArchived(projectID string) ([]model.ProjectItemInProject, error) {
	return s.local.ListArchived(projectID)
}

func (s *SyncBackend) CanUndo() (bool, error) {
	return s.local.CanUndo()
}

// --- Write methods: delegate to local, then queue + notify ---

func (s *SyncBackend) CreateProject(input model.CreateProject) (*model.Project, error) {
	result, err := s.local.CreateProject(input)
	if err != nil {
		return nil, err
	}
	_ = s.engine.QueueOp(OpCreateProject, result.ID, createProjectPayload{
		ID:          result.ID,
		Name:        input.Name,
		Description: input.Description,
	})
	s.engine.Notify()
	return result, nil
}

func (s *SyncBackend) UpdateProject(id string, input model.UpdateProject) (*model.Project, error) {
	result, err := s.local.UpdateProject(id, input)
	if err != nil {
		return nil, err
	}
	_ = s.engine.QueueOp(OpUpdateProject, id, input)
	s.engine.Notify()
	return result, nil
}

func (s *SyncBackend) DeleteProject(id string) error {
	if err := s.local.DeleteProject(id); err != nil {
		return err
	}
	_ = s.engine.QueueOp(OpDeleteProject, id, nil)
	s.engine.Notify()
	return nil
}

func (s *SyncBackend) CreateItem(input model.CreateProjectItem) (*model.ProjectItemDetail, error) {
	result, err := s.local.CreateItem(input)
	if err != nil {
		return nil, err
	}
	_ = s.engine.QueueOp(OpCreateItem, result.ID, createItemPayload{
		ID:         result.ID,
		Title:      input.Title,
		Notes:      input.Notes,
		ProjectIDs: input.ProjectIDs,
	})
	s.engine.Notify()
	return result, nil
}

func (s *SyncBackend) UpdateItem(id string, input model.UpdateProjectItem) (*model.ProjectItem, error) {
	result, err := s.local.UpdateItem(id, input)
	if err != nil {
		return nil, err
	}
	_ = s.engine.QueueOp(OpUpdateItem, id, input)
	s.engine.Notify()
	return result, nil
}

func (s *SyncBackend) DeleteItem(id string) error {
	if err := s.local.DeleteItem(id); err != nil {
		return err
	}
	_ = s.engine.QueueOp(OpDeleteItem, id, nil)
	s.engine.Notify()
	return nil
}

func (s *SyncBackend) ReorderItem(itemID, projectID string, newPosition int) error {
	if err := s.local.ReorderItem(itemID, projectID, newPosition); err != nil {
		return err
	}
	_ = s.engine.QueueOp(OpReorderItem, itemID, reorderPayload{
		ProjectID: projectID,
		Position:  newPosition,
	})
	s.engine.Notify()
	return nil
}

func (s *SyncBackend) AddToProject(itemID, projectID string) error {
	if err := s.local.AddToProject(itemID, projectID); err != nil {
		return err
	}
	_ = s.engine.QueueOp(OpAddToProject, itemID, projectIDPayload{ProjectID: projectID})
	s.engine.Notify()
	return nil
}

func (s *SyncBackend) RemoveFromProject(itemID, projectID string) error {
	if err := s.local.RemoveFromProject(itemID, projectID); err != nil {
		return err
	}
	_ = s.engine.QueueOp(OpRemoveFromProject, itemID, projectIDPayload{ProjectID: projectID})
	s.engine.Notify()
	return nil
}

func (s *SyncBackend) AddDependency(itemID, dependsOn string) error {
	if err := s.local.AddDependency(itemID, dependsOn); err != nil {
		return err
	}
	_ = s.engine.QueueOp(OpAddDependency, itemID, depPayload{DependsOnID: dependsOn})
	s.engine.Notify()
	return nil
}

func (s *SyncBackend) RemoveDependency(itemID, dependsOn string) error {
	if err := s.local.RemoveDependency(itemID, dependsOn); err != nil {
		return err
	}
	_ = s.engine.QueueOp(OpRemoveDependency, itemID, depPayload{DependsOnID: dependsOn})
	s.engine.Notify()
	return nil
}

func (s *SyncBackend) CreateTask(itemID string, input model.CreateProjectItemTask) (*model.ProjectItemTask, error) {
	result, err := s.local.CreateTask(itemID, input)
	if err != nil {
		return nil, err
	}
	_ = s.engine.QueueOp(OpCreateTask, result.ID, taskPayload{
		ItemID: itemID,
		Title:  input.Title,
	})
	s.engine.Notify()
	return result, nil
}

func (s *SyncBackend) UpdateTask(itemID, taskID string, input model.UpdateProjectItemTask) (*model.ProjectItemTask, error) {
	result, err := s.local.UpdateTask(itemID, taskID, input)
	if err != nil {
		return nil, err
	}
	_ = s.engine.QueueOp(OpUpdateTask, taskID, taskUpdatePayload{
		ItemID:    itemID,
		Title:     input.Title,
		Completed: input.Completed,
		Position:  input.Position,
	})
	s.engine.Notify()
	return result, nil
}

func (s *SyncBackend) DeleteTask(itemID, taskID string) error {
	if err := s.local.DeleteTask(itemID, taskID); err != nil {
		return err
	}
	_ = s.engine.QueueOp(OpDeleteTask, taskID, taskRefPayload{ItemID: itemID})
	s.engine.Notify()
	return nil
}

func (s *SyncBackend) CompleteTask(itemID, taskID string) error {
	if err := s.local.CompleteTask(itemID, taskID); err != nil {
		return err
	}
	_ = s.engine.QueueOp(OpCompleteTask, taskID, taskRefPayload{ItemID: itemID})
	s.engine.Notify()
	return nil
}

// Undo is local-only — no sync queue.
func (s *SyncBackend) Undo() (string, error) {
	return s.local.Undo()
}

// --- Payload types for JSON serialization into pending_sync ---

type createProjectPayload struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type createItemPayload struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Notes      *string  `json:"notes,omitempty"`
	ProjectIDs []string `json:"project_ids"`
}

type reorderPayload struct {
	ProjectID string `json:"project_id"`
	Position  int    `json:"position"`
}

type projectIDPayload struct {
	ProjectID string `json:"project_id"`
}

type depPayload struct {
	DependsOnID string `json:"depends_on_id"`
}

type taskPayload struct {
	ItemID string `json:"item_id"`
	Title  string `json:"title"`
}

type taskUpdatePayload struct {
	ItemID    string  `json:"item_id"`
	Title     *string `json:"title,omitempty"`
	Completed *bool   `json:"completed,omitempty"`
	Position  *int    `json:"position,omitempty"`
}

type taskRefPayload struct {
	ItemID string `json:"item_id"`
}
