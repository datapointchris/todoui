package testapi

import (
	"encoding/json"
	"net/http"

	"github.com/datapointchris/todoui/internal/model"
)

func (s *Server) listAllItems(w http.ResponseWriter, _ *http.Request) {
	items, err := s.backend.ListAllItems()
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) listItemsByProject(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseID(r, "projectID")
	if err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid project ID")
		return
	}

	archived := r.URL.Query().Get("archived") == "true"

	if archived {
		items, err := s.backend.ListArchived(projectID)
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	} else {
		items, err := s.backend.ListItemsByProject(projectID)
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	var input model.CreateProjectItem
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if input.Title == "" {
		writeDetail(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(input.ProjectIDs) == 0 {
		writeDetail(w, http.StatusBadRequest, "at least one project_id is required")
		return
	}

	item, err := s.backend.CreateItem(input)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "itemID")
	if err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid item ID")
		return
	}

	item, err := s.backend.GetItem(id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "itemID")
	if err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid item ID")
		return
	}

	var input model.UpdateProjectItem
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	item, err := s.backend.UpdateItem(id, input)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "itemID")
	if err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid item ID")
		return
	}

	if err := s.backend.DeleteItem(id); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := parseID(r, "itemID")
	if err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid item ID")
		return
	}

	var body struct {
		ProjectID int64 `json:"project_id"`
		Position  int   `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := s.backend.ReorderItem(itemID, body.ProjectID, body.Position); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
