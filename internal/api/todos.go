package api

import (
	"encoding/json"
	"net/http"

	"github.com/datapointchris/todoui/internal/model"
)

func (s *Server) listTodosByProject(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseID(r, "projectID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project ID")
		return
	}

	archived := r.URL.Query().Get("archived") == "true"

	var todos []model.Todo
	if archived {
		todos, err = s.backend.ListArchived(projectID)
	} else {
		todos, err = s.backend.ListTodos(projectID)
	}
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, todos)
}

func (s *Server) createTodo(w http.ResponseWriter, r *http.Request) {
	var input model.CreateTodo
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if input.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(input.ProjectIDs) == 0 {
		writeError(w, http.StatusBadRequest, "at least one project_id is required")
		return
	}

	todo, err := s.backend.CreateTodo(input)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, todo)
}

func (s *Server) getTodo(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "todoID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid todo ID")
		return
	}

	todo, err := s.backend.GetTodo(id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, todo)
}

func (s *Server) updateTodo(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "todoID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid todo ID")
		return
	}

	var input model.UpdateTodo
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	todo, err := s.backend.UpdateTodo(id, input)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, todo)
}

func (s *Server) deleteTodo(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "todoID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid todo ID")
		return
	}

	if err := s.backend.DeleteTodo(id); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderTodo(w http.ResponseWriter, r *http.Request) {
	todoID, err := parseID(r, "todoID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid todo ID")
		return
	}

	var body struct {
		ProjectID int64 `json:"project_id"`
		Position  int   `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := s.backend.ReorderTodo(todoID, body.ProjectID, body.Position); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
