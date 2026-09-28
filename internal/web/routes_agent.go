package web

import (
	"net/http"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/ui"
	"github.com/morphis/gummi/internal/webapi"
)

func (s *Server) agentRoutes() {
	s.api("GET /api/agent", s.handleAgent)
	s.api("POST /api/agent/open", s.handleAgentOpen)
	s.api("POST /api/agent/send", s.handleAgentSend)
	s.api("POST /api/agent/interrupt", s.handleAgentInterrupt)
	s.api("POST /api/agent/profile", s.handleAgentProfile)
}

// handleAgent is GET /api/agent: the board session as the agent tab
// draws it.
func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	var a webapi.Agent
	if !s.do(w, r, func(m *ui.Shell) tea.Cmd { a = m.WebAgent(); return nil }) {
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// agentIntent runs one of the tab's gestures, then tells every page the
// session moved: some of them (a clear, a switch) change it inside the
// loop, where no engine event follows to say so.
func (s *Server) agentIntent(w http.ResponseWriter, r *http.Request, fn func(m *ui.Shell) (tea.Cmd, error)) {
	s.intent(w, r, fn)
	s.Publish(webapi.Change{Kind: webapi.ChangeAgent})
}

// handleAgentOpen is POST /api/agent/open.
func (s *Server) handleAgentOpen(w http.ResponseWriter, r *http.Request) {
	var req webapi.AgentOpenRequest
	if !readBody(w, r, &req) {
		return
	}
	s.agentIntent(w, r, func(m *ui.Shell) (tea.Cmd, error) { return m.WebAgentOpen(req) })
}

// handleAgentSend is POST /api/agent/send: a turn, or 409 busy with the
// line handed back while the agent is mid-turn.
func (s *Server) handleAgentSend(w http.ResponseWriter, r *http.Request) {
	var req webapi.AgentSendRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request body: "+err.Error())
		return
	}
	s.agentIntent(w, r, func(m *ui.Shell) (tea.Cmd, error) { return m.WebAgentSend(req.Text) })
}

// handleAgentInterrupt is POST /api/agent/interrupt.
func (s *Server) handleAgentInterrupt(w http.ResponseWriter, r *http.Request) {
	s.agentIntent(w, r, func(m *ui.Shell) (tea.Cmd, error) { return m.WebAgentInterrupt() })
}

// handleAgentProfile is POST /api/agent/profile: /profile and /model.
func (s *Server) handleAgentProfile(w http.ResponseWriter, r *http.Request) {
	var req webapi.AgentProfileRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request body: "+err.Error())
		return
	}
	s.agentIntent(w, r, func(m *ui.Shell) (tea.Cmd, error) { return m.WebAgentProfile(req) })
}
