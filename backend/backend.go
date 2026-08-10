package backend

import "github.com/datapointchris/todoui/v2/model"

// Backend defines the interface for all data operations.
// LocalBackend provides direct SQLite access; SyncBackend wraps it with background push/pull.
type Backend interface {
	// Projects
	// ListProjects is the active projects. Closing a project is what takes it
	// out of the list, so the default has to hide the terminal ones or
	// completing one would do nothing visible.
	ListProjects() ([]model.ProjectWithItemCount, error)
	// ListProjectsByStatus takes a status name or model.StatusAll.
	ListProjectsByStatus(status string) ([]model.ProjectWithItemCount, error)
	GetProject(id string) (*model.ProjectWithItemCount, error)
	CreateProject(input model.CreateProject) (*model.Project, error)
	UpdateProject(id string, input model.UpdateProject) (*model.Project, error)
	// SetProjectStatus moves a project between active, done, and dropped. The
	// closing timestamp and the reason are derived from the transition rather
	// than sent, so they cannot contradict the status they describe.
	SetProjectStatus(id, status string, reason *string) (*model.Project, error)
	DeleteProject(id string) error
	ReorderProject(projectID string, newPosition int) error

	// Items
	ListAllItems() ([]model.ProjectItem, error)
	// ListItemsByRepo narrows to one repo's work. A nil repo is the untagged
	// items — the ones that are not repo work at all — which is a different
	// question from "everything" and is why this takes a pointer.
	ListItemsByRepo(repo *string) ([]model.ProjectItem, error)
	// ListAllItemsIncludingArchived is what resolves a short id: an archived
	// item still has to be addressable, or nothing can unarchive it.
	ListAllItemsIncludingArchived() ([]model.ProjectItem, error)
	ListItemsByProject(projectID string) ([]model.ProjectItemInProject, error)
	GetItem(id string) (*model.ProjectItemDetail, error)
	CreateItem(input model.CreateProjectItem) (*model.ProjectItemDetail, error)
	UpdateItem(id string, input model.UpdateProjectItem) (*model.ProjectItem, error)
	DeleteItem(id string) error
	ReorderItem(itemID, projectID string, newPosition int) error

	// Multi-project membership
	AddToProject(itemID, projectID string) error
	RemoveFromProject(itemID, projectID string) error
	GetItemProjects(itemID string) ([]model.Project, error)

	// Dependencies
	AddDependency(itemID, dependsOn string) error
	RemoveDependency(itemID, dependsOn string) error
	GetBlockers(itemID string) ([]model.ProjectItem, error)

	// Tasks
	ListTasks(itemID string) ([]model.ProjectItemTask, error)
	CreateTask(itemID string, input model.CreateProjectItemTask) (*model.ProjectItemTask, error)
	UpdateTask(itemID, taskID string, input model.UpdateProjectItemTask) (*model.ProjectItemTask, error)
	DeleteTask(itemID, taskID string) error
	CompleteTask(itemID, taskID string) error

	// Search
	Search(query string) ([]model.ProjectItem, error)
	SearchByRepo(query string, repo *string) ([]model.ProjectItem, error)

	// Filters
	ListBlocked() ([]model.ProjectItem, error)
	ListArchived(projectID string) ([]model.ProjectItemInProject, error)

	// Undo
	Undo() (*model.UndoResult, error)
	CanUndo() (bool, error)
}
