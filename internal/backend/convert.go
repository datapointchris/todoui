package backend

import (
	"database/sql"
	"time"

	"github.com/datapointchris/todoui/internal/db/generated"
	"github.com/datapointchris/todoui/internal/model"
)

const (
	timeLayout = "2006-01-02 15:04:05"
	dateLayout = "2006-01-02"
)

func toModelProject(p generated.Project) model.Project {
	t, _ := time.Parse(timeLayout, p.CreatedAt)
	return model.Project{
		ID:        p.ID,
		Name:      p.Name,
		Position:  int(p.Position),
		CreatedAt: t,
	}
}

func toModelProjects(ps []generated.Project) []model.Project {
	out := make([]model.Project, len(ps))
	for i, p := range ps {
		out[i] = toModelProject(p)
	}
	return out
}

func toModelTodo(t generated.Todo) model.Todo {
	todo := model.Todo{
		ID:        t.ID,
		Title:     t.Title,
		Completed: t.Completed != 0,
		Archived:  t.Archived != 0,
	}
	if ct, err := time.Parse(timeLayout, t.CreatedAt); err == nil {
		todo.CreatedAt = ct
	}
	if ut, err := time.Parse(timeLayout, t.UpdatedAt); err == nil {
		todo.UpdatedAt = ut
	}
	if t.Notes.Valid {
		todo.Notes = &t.Notes.String
	}
	if t.DueDate.Valid {
		if dt, err := time.Parse(dateLayout, t.DueDate.String); err == nil {
			todo.DueDate = &dt
		}
	}
	return todo
}

func toModelTodos(ts []generated.Todo) []model.Todo {
	out := make([]model.Todo, len(ts))
	for i, t := range ts {
		out[i] = toModelTodo(t)
	}
	return out
}

func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func timeToNullString(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.Format(dateLayout), Valid: true}
}

func boolToInt64(b *bool) int64 {
	if b == nil || !*b {
		return 0
	}
	return 1
}
