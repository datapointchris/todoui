package backend

import "github.com/datapointchris/todoui/internal/model"

// Backend defines the interface for all data operations.
// Two implementations exist: LocalBackend (embedded SQLite) and RemoteBackend (HTTP client).
type Backend interface {
	// Projects
	ListProjects() ([]model.ProjectWithItemCount, error)
	GetProject(id int64) (*model.ProjectWithItemCount, error)
	CreateProject(name string) (*model.Project, error)
	UpdateProject(id int64, input model.UpdateProject) (*model.Project, error)
	DeleteProject(id int64) error

	// Items
	ListAllItems() ([]model.ProjectItem, error)
	ListItemsByProject(projectID int64) ([]model.ProjectItemInProject, error)
	GetItem(id int64) (*model.ProjectItemDetail, error)
	CreateItem(input model.CreateProjectItem) (*model.ProjectItemDetail, error)
	UpdateItem(id int64, input model.UpdateProjectItem) (*model.ProjectItem, error)
	DeleteItem(id int64) error
	ReorderItem(itemID, projectID int64, newPosition int) error

	// Multi-project membership
	AddToProject(itemID, projectID int64) error
	RemoveFromProject(itemID, projectID int64) error
	GetItemProjects(itemID int64) ([]model.Project, error)

	// Dependencies
	AddDependency(itemID, dependsOn int64) error
	RemoveDependency(itemID, dependsOn int64) error
	GetBlockers(itemID int64) ([]model.ProjectItem, error)

	// Search
	Search(query string) ([]model.ProjectItem, error)

	// Filters
	ListBlocked() ([]model.ProjectItem, error)
	ListArchived(projectID int64) ([]model.ProjectItemInProject, error)

	// Undo
	Undo() (string, error)
	CanUndo() (bool, error)
}
