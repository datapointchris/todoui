package backend

import (
	"database/sql"
	"time"

	"github.com/datapointchris/todoui/internal/db/generated"
	"github.com/datapointchris/todoui/internal/model"
)

const timeLayout = "2006-01-02 15:04:05"

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

func toModelProjectWithItemCount(row generated.ListProjectsWithItemCountRow) model.ProjectWithItemCount {
	t, _ := time.Parse(timeLayout, row.CreatedAt)
	return model.ProjectWithItemCount{
		Project: model.Project{
			ID:        row.ID,
			Name:      row.Name,
			Position:  int(row.Position),
			CreatedAt: t,
		},
		ItemCount: int(row.ItemCount),
	}
}

func toModelProjectWithItemCountFromGet(row generated.GetProjectWithItemCountRow) model.ProjectWithItemCount {
	t, _ := time.Parse(timeLayout, row.CreatedAt)
	return model.ProjectWithItemCount{
		Project: model.Project{
			ID:        row.ID,
			Name:      row.Name,
			Position:  int(row.Position),
			CreatedAt: t,
		},
		ItemCount: int(row.ItemCount),
	}
}

func toModelProjectItem(pi generated.ProjectItem) model.ProjectItem {
	item := model.ProjectItem{
		ID:        pi.ID,
		Title:     pi.Title,
		Completed: pi.Completed != 0,
		Archived:  pi.Archived != 0,
	}
	if ct, err := time.Parse(timeLayout, pi.CreatedAt); err == nil {
		item.CreatedAt = ct
	}
	if ut, err := time.Parse(timeLayout, pi.UpdatedAt); err == nil {
		item.UpdatedAt = ut
	}
	if pi.Notes.Valid {
		item.Notes = &pi.Notes.String
	}
	return item
}

func toModelProjectItems(pis []generated.ProjectItem) []model.ProjectItem {
	out := make([]model.ProjectItem, len(pis))
	for i, pi := range pis {
		out[i] = toModelProjectItem(pi)
	}
	return out
}

func toModelProjectItemInProject(row generated.ListItemsByProjectRow) model.ProjectItemInProject {
	item := model.ProjectItem{
		ID:        row.ID,
		Title:     row.Title,
		Completed: row.Completed != 0,
		Archived:  row.Archived != 0,
	}
	if ct, err := time.Parse(timeLayout, row.CreatedAt); err == nil {
		item.CreatedAt = ct
	}
	if ut, err := time.Parse(timeLayout, row.UpdatedAt); err == nil {
		item.UpdatedAt = ut
	}
	if row.Notes.Valid {
		item.Notes = &row.Notes.String
	}
	return model.ProjectItemInProject{
		ProjectItem: item,
		Position:    int(row.MembershipPosition),
	}
}

func toModelProjectItemInProjectFromArchived(row generated.ListArchivedItemsRow) model.ProjectItemInProject {
	item := model.ProjectItem{
		ID:        row.ID,
		Title:     row.Title,
		Completed: row.Completed != 0,
		Archived:  row.Archived != 0,
	}
	if ct, err := time.Parse(timeLayout, row.CreatedAt); err == nil {
		item.CreatedAt = ct
	}
	if ut, err := time.Parse(timeLayout, row.UpdatedAt); err == nil {
		item.UpdatedAt = ut
	}
	if row.Notes.Valid {
		item.Notes = &row.Notes.String
	}
	return model.ProjectItemInProject{
		ProjectItem: item,
		Position:    int(row.MembershipPosition),
	}
}

func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func boolToInt64(b *bool) int64 {
	if b == nil || !*b {
		return 0
	}
	return 1
}
