package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/model"
)

// Server holds the dependencies for the HTTP API.
type Server struct {
	backend backend.Backend
	router  chi.Router
}

// NewServer creates an API server with all routes registered.
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
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	r.Use(jsonContentType)

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

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

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, model.ErrDuplicateName):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, model.ErrCyclicDependency):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, model.ErrLastProject):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, model.ErrNothingToUndo):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func parseID(r *http.Request, param string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, param), 10, 64)
}
