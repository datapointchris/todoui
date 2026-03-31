package testapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/model"
)

// Server is a test double that mirrors ichrisbirch FastAPI behavior.
// It is used only in tests — the real API is ichrisbirch.
type Server struct {
	backend backend.Backend
	router  chi.Router
}

// NewServer creates a test API server with all routes registered.
func NewServer(b backend.Backend) *Server {
	s := &Server{backend: b}
	s.router = s.buildRouter()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(jsonContentType)

	r.Route("/projects", func(r chi.Router) {
		r.Get("/", s.listProjects)
		r.Post("/", s.createProject)
		r.Route("/{projectID}", func(r chi.Router) {
			r.Get("/", s.getProject)
			r.Patch("/", s.updateProject)
			r.Delete("/", s.deleteProject)
			r.Get("/items", s.listItemsByProject)
		})
	})

	r.Route("/project-items", func(r chi.Router) {
		r.Get("/", s.listAllItems)
		r.Post("/", s.createItem)
		r.Get("/blocked", s.listBlocked)
		r.Get("/search", s.search)
		r.Route("/{itemID}", func(r chi.Router) {
			r.Get("/", s.getItem)
			r.Patch("/", s.updateItem)
			r.Delete("/", s.deleteItem)
			r.Patch("/reorder", s.reorderItem)
			r.Get("/projects", s.getItemProjects)
			r.Post("/projects", s.addToProject)
			r.Delete("/projects/{projectID}", s.removeFromProject)
			r.Post("/dependencies", s.addDependency)
			r.Delete("/dependencies/{depID}", s.removeDependency)
			r.Get("/blockers", s.getBlockers)
			r.Get("/tasks/", s.listTasks)
			r.Post("/tasks/", s.createTask)
			r.Route("/tasks/{taskID}", func(r chi.Router) {
				r.Patch("/", s.updateTask)
				r.Delete("/", s.deleteTask)
				r.Post("/complete/", s.completeTask)
			})
		})
	})

	r.Route("/undo", func(r chi.Router) {
		r.Get("/", s.canUndo)
		r.Post("/", s.undo)
	})

	return r
}

// --- Helpers ---

func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encoding response: %v", err)
	}
}

// writeDetail matches FastAPI's error response format: {"detail": "message"}
func writeDetail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"detail": msg})
}

// handleError maps domain errors to HTTP responses matching ichrisbirch FastAPI.
func handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeDetail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, model.ErrDuplicateName):
		writeDetail(w, http.StatusConflict, err.Error())
	case errors.Is(err, model.ErrCyclicDependency):
		writeDetail(w, http.StatusConflict, err.Error())
	case errors.Is(err, model.ErrLastProject):
		writeDetail(w, http.StatusConflict, err.Error())
	case errors.Is(err, model.ErrNothingToUndo):
		writeDetail(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("internal error: %v", err)
		writeDetail(w, http.StatusInternalServerError, "internal server error")
	}
}

func getParam(r *http.Request, param string) string {
	return chi.URLParam(r, param)
}
