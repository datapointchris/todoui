package testapi

import (
	"encoding/json"
	"net/http"

	"github.com/datapointchris/todoui/internal/model"
)

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	itemID := getParam(r, "itemID")

	tasks, err := s.backend.ListTasks(itemID)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	itemID := getParam(r, "itemID")

	var input model.CreateProjectItemTask
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if input.Title == "" {
		writeDetail(w, http.StatusBadRequest, "title is required")
		return
	}

	task, err := s.backend.CreateTask(itemID, input)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	itemID := getParam(r, "itemID")
	taskID := getParam(r, "taskID")

	var input model.UpdateProjectItemTask
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeDetail(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	task, err := s.backend.UpdateTask(itemID, taskID, input)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	itemID := getParam(r, "itemID")
	taskID := getParam(r, "taskID")

	if err := s.backend.DeleteTask(itemID, taskID); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) completeTask(w http.ResponseWriter, r *http.Request) {
	itemID := getParam(r, "itemID")
	taskID := getParam(r, "taskID")

	if err := s.backend.CompleteTask(itemID, taskID); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
