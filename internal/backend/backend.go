package backend

import "github.com/datapointchris/todoui/internal/model"

// Backend defines the interface for all data operations.
// Two implementations exist: LocalBackend (embedded SQLite) and RemoteBackend (HTTP client).
type Backend interface {
	// Projects
	ListProjects() ([]model.Project, error)
	CreateProject(name string) (*model.Project, error)
	DeleteProject(id int64) error
	ReorderProject(id int64, newPosition int) error

	// Todos
	ListTodos(projectID int64) ([]model.Todo, error)
	GetTodo(id int64) (*model.Todo, error)
	CreateTodo(input model.CreateTodo) (*model.Todo, error)
	UpdateTodo(id int64, input model.UpdateTodo) (*model.Todo, error)
	DeleteTodo(id int64) error
	ReorderTodo(todoID, projectID int64, newPosition int) error

	// Multi-project membership
	AddToProject(todoID, projectID int64) error
	RemoveFromProject(todoID, projectID int64) error
	GetTodoProjects(todoID int64) ([]model.Project, error)

	// Dependencies
	AddDependency(todoID, dependsOn int64) error
	RemoveDependency(todoID, dependsOn int64) error
	GetBlockers(todoID int64) ([]model.Todo, error)

	// Search
	Search(query string) ([]model.Todo, error)

	// Filters
	ListToday() ([]model.Todo, error)
	ListBlocked() ([]model.Todo, error)
	ListArchived(projectID int64) ([]model.Todo, error)

	// Undo
	Undo() (string, error)
	CanUndo() (bool, error)
}
