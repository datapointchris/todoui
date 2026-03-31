package backend

import "github.com/datapointchris/todoui/internal/model"

// Backend defines the interface for all data operations.
// Two implementations exist: LocalBackend (embedded SQLite) and RemoteBackend (HTTP client).
type Backend interface {
	// Projects
	ListProjects() ([]model.ProjectWithItemCount, error)
	GetProject(id string) (*model.ProjectWithItemCount, error)
	CreateProject(input model.CreateProject) (*model.Project, error)
	UpdateProject(id string, input model.UpdateProject) (*model.Project, error)
	DeleteProject(id string) error

	// Items
	ListAllItems() ([]model.ProjectItem, error)
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

	// Filters
	ListBlocked() ([]model.ProjectItem, error)
	ListArchived(projectID string) ([]model.ProjectItemInProject, error)

	// Undo
	Undo() (string, error)
	CanUndo() (bool, error)
}
