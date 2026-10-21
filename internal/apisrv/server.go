// Package apisrv is the documented JSON HTTP API (v1) for the UI. It is a thin
// adapter over *training.Service: it parses requests, maps HTTP preconditions
// and errors, and renders JSON. Every rule about workouts, sets, plans and
// exercises lives in the service, which the MCP adapter shares.
//
// The handler expects full request paths beginning with /api/v1/. Authentication
// is applied by the outer layer (httpsrv.Bearer); this package does none.
package apisrv

import (
	_ "embed"
	"log"
	"net/http"
	"strings"

	"github.com/fullfran/training-mcp/internal/training"
)

// APIVersion is reported by GET /api/v1/config.
const APIVersion = "v1"

// Prefix is the path every route lives under.
const Prefix = "/api/v1"

// maxBodyBytes bounds every JSON request body. Video uploads are bounded by
// the service's MaxVideoBytes instead.
const maxBodyBytes = 1 << 20

//go:embed openapi.yaml
var openAPISpec []byte

// OpenAPISpec returns the embedded OpenAPI 3.1 document.
func OpenAPISpec() []byte { return append([]byte(nil), openAPISpec...) }

// Server serves the JSON API.
type Server struct {
	svc    *training.Service
	mux    *http.ServeMux
	logger *log.Logger
	routes []route
}

type route struct {
	method string
	path   string // full path pattern, e.g. /api/v1/sets/{id}
}

// New builds the API over svc.
func New(svc *training.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux(), logger: log.Default()}
	s.register()
	return s
}

// Handler returns the API handler. Mount it at "/api/v1/" on the outer mux.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) register() {
	s.handle("GET", "/config", s.getConfig)
	s.handle("GET", "/openapi.yaml", s.getOpenAPI)

	s.handle("GET", "/workouts", s.listWorkouts)
	s.handle("GET", "/workouts/{date}", s.getWorkout)
	s.handle("POST", "/workouts/{date}/sets", s.logSets)
	s.handle("POST", "/workouts/{date}/plan", s.followPlan)
	s.handle("PATCH", "/workouts/{date}/prescriptions/{exercise}", s.patchPrescription)
	s.handle("DELETE", "/workouts/{date}/prescriptions/{exercise}", s.deletePrescription)

	s.handle("GET", "/sets/{id}", s.getSet)
	s.handle("PATCH", "/sets/{id}", s.patchSet)
	s.handle("DELETE", "/sets/{id}", s.deleteSet)
	s.handle("GET", "/sets/{id}/videos", s.listSetVideos)
	s.handle("POST", "/sets/{id}/videos", s.uploadSetVideo)

	s.handle("GET", "/plans", s.listPlans)
	s.handle("POST", "/plans", s.createPlan)
	s.handle("POST", "/plans/import", s.importPlan)
	s.handle("GET", "/plans/{id}", s.getPlan)
	s.handle("PUT", "/plans/{id}", s.replacePlan)
	s.handle("DELETE", "/plans/{id}", s.deletePlan)
	s.handle("GET", "/plans/{id}/export", s.exportPlan)
	s.handle("PUT", "/plans/{id}/schedule", s.putPlanSchedule)
	s.handle("GET", "/schedule", s.getSchedule)

	s.handle("GET", "/exercises", s.listExercises)
	s.handle("GET", "/exercises/{ref}", s.getExercise)
	s.handle("PATCH", "/exercises/{ref}", s.renameExercise)
	s.handle("POST", "/exercises/{ref}/aliases", s.addAlias)
	s.handle("DELETE", "/exercises/{ref}/aliases/{alias}", s.removeAlias)
	s.handle("GET", "/exercises/{ref}/history", s.exerciseHistory)
	s.handle("GET", "/exercises/{ref}/stats", s.exerciseStats)
	s.handle("GET", "/exercises/{ref}/videos", s.listExerciseVideos)
	s.handle("POST", "/exercises/{ref}/videos", s.uploadExerciseVideo)
	s.handle("GET", "/exercises/{ref}/guide", s.exerciseGuide)
	s.handle("PUT", "/exercises/{ref}/instructions", s.putInstructions)

	s.handle("GET", "/media/{id}", s.getMedia)
	s.handle("DELETE", "/media/{id}", s.deleteMedia)

	s.handle("GET", "/stats/summary", s.statsSummary)

	// Anything else under the prefix is a JSON 404, or a JSON 405 when the path
	// exists for other methods.
	s.mux.HandleFunc(Prefix+"/", s.fallback)
}

// apiFunc handles one request; a returned error is rendered by writeError.
type apiFunc func(w http.ResponseWriter, r *http.Request) error

func (s *Server) handle(method, path string, h apiFunc) {
	full := Prefix + path
	s.routes = append(s.routes, route{method: method, path: full})
	s.mux.HandleFunc(method+" "+full, func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.logger.Printf("apisrv: panic in %s %s: %v", r.Method, r.URL.Path, v)
				writeJSON(w, http.StatusInternalServerError, errorBody{Error: apiError{Code: codeInternal, Message: "internal server error"}})
			}
		}()
		w.Header().Set("Cache-Control", "no-store")
		if err := h(w, r); err != nil {
			s.writeError(w, r, err)
		}
	})
}

var probeMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

func (s *Server) fallback(w http.ResponseWriter, r *http.Request) {
	var allowed []string
	for _, m := range probeMethods {
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, pattern := s.mux.Handler(probe); pattern != "" && pattern != Prefix+"/" {
			allowed = append(allowed, m)
		}
	}
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		s.writeError(w, r, &apiError{status: http.StatusMethodNotAllowed, Code: codeMethodNotAllowed,
			Message: r.Method + " is not supported on this path; allowed: " + strings.Join(allowed, ", ")})
		return
	}
	s.writeError(w, r, &apiError{status: http.StatusNotFound, Code: codeNotFound, Message: "no such endpoint: " + r.URL.Path})
}
