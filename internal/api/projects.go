package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.backend.ListProjects()
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	project, err := s.backend.CreateProject(body.Name)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "projectID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project ID")
		return
	}

	if err := s.backend.DeleteProject(id); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderProject(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "projectID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project ID")
		return
	}

	var body struct {
		Position int `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := s.backend.ReorderProject(id, body.Position); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
