package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) addDependency(w http.ResponseWriter, r *http.Request) {
	todoID, err := parseID(r, "todoID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid todo ID")
		return
	}

	var body struct {
		DependsOnID int64 `json:"depends_on_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := s.backend.AddDependency(todoID, body.DependsOnID); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) removeDependency(w http.ResponseWriter, r *http.Request) {
	todoID, err := parseID(r, "todoID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid todo ID")
		return
	}

	depID, err := parseID(r, "depID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid dependency ID")
		return
	}

	if err := s.backend.RemoveDependency(todoID, depID); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getBlockers(w http.ResponseWriter, r *http.Request) {
	todoID, err := parseID(r, "todoID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid todo ID")
		return
	}

	blockers, err := s.backend.GetBlockers(todoID)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, blockers)
}

func (s *Server) getTodoProjects(w http.ResponseWriter, r *http.Request) {
	todoID, err := parseID(r, "todoID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid todo ID")
		return
	}

	projects, err := s.backend.GetTodoProjects(todoID)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) addToProject(w http.ResponseWriter, r *http.Request) {
	todoID, err := parseID(r, "todoID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid todo ID")
		return
	}

	var body struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := s.backend.AddToProject(todoID, body.ProjectID); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) removeFromProject(w http.ResponseWriter, r *http.Request) {
	todoID, err := parseID(r, "todoID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid todo ID")
		return
	}

	projectID, err := parseID(r, "projectID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project ID")
		return
	}

	if err := s.backend.RemoveFromProject(todoID, projectID); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	results, err := s.backend.Search(q)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) listToday(w http.ResponseWriter, _ *http.Request) {
	todos, err := s.backend.ListToday()
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, todos)
}

func (s *Server) listBlocked(w http.ResponseWriter, _ *http.Request) {
	todos, err := s.backend.ListBlocked()
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, todos)
}

func (s *Server) canUndo(w http.ResponseWriter, _ *http.Request) {
	ok, err := s.backend.CanUndo()
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"can_undo": ok})
}

func (s *Server) undo(w http.ResponseWriter, _ *http.Request) {
	desc, err := s.backend.Undo()
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"description": desc})
}
