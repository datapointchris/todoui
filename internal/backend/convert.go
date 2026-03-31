package backend

import (
	"database/sql"
	"time"

	"github.com/datapointchris/todoui/internal/db/generated"
	"github.com/datapointchris/todoui/internal/model"
)

func parseTime(s string) time.Time {
	// Try RFC3339Nano first (ISO 8601 from API), then SQLite format as fallback
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05.000Z", s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t
	}
	return time.Time{}
}

func toModelProject(p generated.Project) model.Project {
	proj := model.Project{
		ID:        p.ID,
		Name:      p.Name,
		Position:  int(p.Position),
		CreatedAt: parseTime(p.CreatedAt),
	}
	if p.Description.Valid {
		proj.Description = &p.Description.String
	}
	return proj
}

func toModelProjects(ps []generated.Project) []model.Project {
	out := make([]model.Project, len(ps))
	for i, p := range ps {
		out[i] = toModelProject(p)
	}
	return out
}

func toModelProjectWithItemCount(row generated.ListProjectsWithItemCountRow) model.ProjectWithItemCount {
	proj := model.Project{
		ID:        row.ID,
		Name:      row.Name,
		Position:  int(row.Position),
		CreatedAt: parseTime(row.CreatedAt),
	}
	if row.Description.Valid {
		proj.Description = &row.Description.String
	}
	return model.ProjectWithItemCount{
		Project:   proj,
		ItemCount: int(row.ItemCount),
	}
}

func toModelProjectWithItemCountFromGet(row generated.GetProjectWithItemCountRow) model.ProjectWithItemCount {
	proj := model.Project{
		ID:        row.ID,
		Name:      row.Name,
		Position:  int(row.Position),
		CreatedAt: parseTime(row.CreatedAt),
	}
	if row.Description.Valid {
		proj.Description = &row.Description.String
	}
	return model.ProjectWithItemCount{
		Project:   proj,
		ItemCount: int(row.ItemCount),
	}
}

func toModelProjectItem(pi generated.ProjectItem) model.ProjectItem {
	item := model.ProjectItem{
		ID:        pi.ID,
		Title:     pi.Title,
		Completed: pi.Completed != 0,
		Archived:  pi.Archived != 0,
		CreatedAt: parseTime(pi.CreatedAt),
		UpdatedAt: parseTime(pi.UpdatedAt),
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
		CreatedAt: parseTime(row.CreatedAt),
		UpdatedAt: parseTime(row.UpdatedAt),
	}
	if row.Notes.Valid {
		item.Notes = &row.Notes.String
	}
	return model.ProjectItemInProject{
		ProjectItem:  item,
		Position:     int(row.MembershipPosition),
		ProjectCount: int(row.ProjectCount),
	}
}

func toModelProjectItemInProjectFromArchived(row generated.ListArchivedItemsRow) model.ProjectItemInProject {
	item := model.ProjectItem{
		ID:        row.ID,
		Title:     row.Title,
		Completed: row.Completed != 0,
		Archived:  row.Archived != 0,
		CreatedAt: parseTime(row.CreatedAt),
		UpdatedAt: parseTime(row.UpdatedAt),
	}
	if row.Notes.Valid {
		item.Notes = &row.Notes.String
	}
	return model.ProjectItemInProject{
		ProjectItem: item,
		Position:    int(row.MembershipPosition),
	}
}

func toModelProjectItemTask(t generated.ProjectItemTask) model.ProjectItemTask {
	return model.ProjectItemTask{
		ID:        t.ID,
		ItemID:    t.ItemID,
		Title:     t.Title,
		Completed: t.Completed != 0,
		Position:  int(t.Position),
		CreatedAt: parseTime(t.CreatedAt),
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
