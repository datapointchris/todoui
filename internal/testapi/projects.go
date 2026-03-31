package testapi

import (
	"encoding/json"
	"net/http"

	"github.com/datapointchris/todoui/internal/model"
)

func (s *Server) listProjects(w http.ResponseWriter, _ *http.Request) {
	projects, err := s.backend.ListProjects()
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id := getParam(r, "projectID")
	project, err := s.backend.GetProject(id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var input model.CreateProject
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if input.Name == "" {
		writeDetail(w, http.StatusBadRequest, "name is required")
		return
	}

	project, err := s.backend.CreateProject(input)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id := getParam(r, "projectID")

	var input model.UpdateProject
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	project, err := s.backend.UpdateProject(id, input)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := getParam(r, "projectID")
	if err := s.backend.DeleteProject(id); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
