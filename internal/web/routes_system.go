package web

import "net/http"

func (s *Server) systemRoutes() {
	s.api("GET /api/doctor", s.handleDoctor)
	s.api("POST /api/doctor", s.handleDeepDoctor)
}

// handleDoctor is GET /api/doctor: the checklist `gummi doctor --json`
// prints, from the command package's own builder.
//
// The deep run is not a GET. It asks every backend for a model turn, and
// a GET passes no same-origin check — any page on another port of this
// host could spend it with an <img> — so ?deep=1 here is refused and the
// page POSTs it instead.
func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if s.opt.Doctor == nil {
		notYet(w, r)
		return
	}
	if r.URL.Query().Get("deep") == "1" {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "deep checks spend a model turn per role; POST /api/doctor?deep=1 runs them")
		return
	}
	writeJSON(w, http.StatusOK, s.opt.Doctor(r))
}

// handleDeepDoctor is POST /api/doctor?deep=1: `gummi doctor --deep`, the
// live probe of every model the profiles name. As a write it passes the
// same-origin check.
func (s *Server) handleDeepDoctor(w http.ResponseWriter, r *http.Request) {
	if s.opt.Doctor == nil {
		notYet(w, r)
		return
	}
	if r.URL.Query().Get("deep") != "1" {
		writeError(w, http.StatusBadRequest, "POST /api/doctor is the deep run; say ?deep=1")
		return
	}
	writeJSON(w, http.StatusOK, s.opt.Doctor(r))
}
