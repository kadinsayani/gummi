package web

import (
	"net/http"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/ui"
	"github.com/morphis/gummi/internal/webapi"
)

func (s *Server) createRoutes() {
	s.api("POST /api/cards", s.handleCreateCard)
	s.api("GET /api/form", s.handleForm)
}

// handleForm is GET /api/form[?repo=]: the new-card form's choices.
func (s *Server) handleForm(w http.ResponseWriter, r *http.Request) {
	var (
		f   webapi.Form
		err error
	)
	repo := r.URL.Query().Get("repo")
	if !s.do(w, r, func(m *ui.Shell) tea.Cmd { f, err = m.WebForm(repo); return nil }) {
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// handleCreateCard is POST /api/cards: the form, submitted. It answers
// the new card.
func (s *Server) handleCreateCard(w http.ResponseWriter, r *http.Request) {
	var body webapi.CreateCardRequest
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "expected the new-card form as JSON")
		return
	}
	c, err := s.opt.Board.CreateCard(r.Context(), body, person(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}
