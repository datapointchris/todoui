package model

import "time"

type Todo struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Notes     *string    `json:"notes,omitempty"`
	DueDate   *time.Time `json:"due_date,omitempty"`
	Completed bool       `json:"completed"`
	Archived  bool       `json:"archived"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Projects  []Project  `json:"projects,omitempty"`
}

type CreateTodo struct {
	Title      string     `json:"title"`
	Notes      *string    `json:"notes,omitempty"`
	DueDate    *time.Time `json:"due_date,omitempty"`
	ProjectIDs []int64    `json:"project_ids"`
}

type UpdateTodo struct {
	Title     *string    `json:"title,omitempty"`
	Notes     *string    `json:"notes,omitempty"`
	DueDate   *time.Time `json:"due_date,omitempty"`
	Completed *bool      `json:"completed,omitempty"`
	Archived  *bool      `json:"archived,omitempty"`
}
