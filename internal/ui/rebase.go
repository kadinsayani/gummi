package ui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/state"
)

// The agent-rebase flow: rebaseFeature's plain rebase stops on
// conflicts → rebaseConflictMsg offers the hand-off → agentRebase
// dispatches the engine's rebase-resolve session → on its idle,
// judgeRebase reads the resulting git state → rebaseSettled re-verifies
// or escalates. Conflict resolution is agent-authored change, so a
// successful rebase of a Verify-stage feature re-runs Verify instead of
// letting the resolution land unseen; earlier stages still have the
// quality floor ahead of them.

// rebaseConflictMsg reports a rebase that stopped on conflicts (and
// self-aborted) while an engine is wired — the hand-off offer.
type rebaseConflictMsg struct {
	f     domain.Feature
	files []string
}

// rebaseSettledMsg carries the judged outcome of a finished
// rebase-resolve session: ok when the branch is rebased and the
// worktree clean; otherwise problem says what the git state shows.
type rebaseSettledMsg struct {
	f       domain.Feature
	ok      bool
	problem string
}

// offerAgentRebase pushes the hand-off confirm for a conflicted rebase.
// An agent session costs credits, so it never starts without a yes.
func (m *Shell) offerAgentRebase(msg rebaseConflictMsg) {
	f, files := msg.f, msg.files
	detail := "runs an agent session in the worktree"
	if f.Stage == domain.StageVerify {
		detail += "; verify re-runs after"
	}
	if len(files) > 0 {
		// git-derived file names; sanitize like every other notice
		detail = sanitize("conflicts: "+strings.Join(files, ", ")) + " — " + detail
	}
	m.Overlay.Push(&confirmDialog{
		card:      f.ID,
		id:        "agent-rebase",
		question:  "rebase " + string(f.ID) + " onto main hit conflicts — let the agent resolve them?",
		detail:    detail,
		onConfirm: func() tea.Cmd { return m.agentRebase(f, files) },
	})
}

// agentRebase dispatches the engine's rebase-resolve session, holding the
// stop the card waits at so an unresolved rebase can put it back.
func (m *Shell) agentRebase(f domain.Feature, files []string) tea.Cmd {
	if it, ok := m.inbox.get(f.ID); ok {
		if m.rebaseHeld == nil {
			m.rebaseHeld = map[domain.FeatureID]attnItem{}
		}
		m.rebaseHeld[f.ID] = it
	}
	return func() tea.Msg {
		if err := m.engine.RunRebase(context.Background(), f, files); err != nil {
			return noticeMsg{text: sanitize(err.Error()), isErr: true}
		}
		return noticeMsg{text: string(f.ID) + ": agent dispatched to rebase onto main"}
	}
}

// judgeRebase reads the git state a finished rebase-resolve session
// left behind. The engine has already aborted anything mid-flight, so a
// clean worktree whose branch now carries main's HEAD is success — the
// agent's own claims are never consulted.
func (m *Shell) judgeRebase(id domain.FeatureID) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		f, err := m.store.GetFeature(ctx, id)
		if err != nil {
			return noticeMsg{text: err.Error(), isErr: true}
		}
		if dirty, err := m.wt.Dirty(ctx, &f); err != nil {
			return noticeMsg{text: sanitize(err.Error()), isErr: true}
		} else if dirty {
			return rebaseSettledMsg{f: f, problem: "the worktree was left dirty"}
		}
		if rebased, err := m.wt.RebasedOnBase(ctx, &f); err != nil {
			return noticeMsg{text: sanitize(err.Error()), isErr: true}
		} else if !rebased {
			return rebaseSettledMsg{f: f, problem: "the branch is still not rebased"}
		}
		return rebaseSettledMsg{f: f, ok: true}
	}
}

// rebaseSettled folds the judged outcome into the board: a failed agent
// rebase escalates to the human; a successful one at Verify re-runs the
// stage (the resolution is unreviewed agent work), and elsewhere the
// workflow's remaining stages already cover it.
func (m *Shell) rebaseSettled(msg rebaseSettledMsg) tea.Cmd {
	id := msg.f.ID
	held, wasHeld := m.rebaseHeld[id]
	delete(m.rebaseHeld, id)
	if !msg.ok {
		// The resolve session took the stage session's place on the engine,
		// and it carries no verdict of its own: left standing, it made the
		// card read as a stage that ended with no clear verdict — at a
		// verify that had passed, "verification stopped here" with "land
		// anyway" on offer, while the store still said verified. Drop it,
		// so the card reads as its stage really ended, as it does after a
		// restart.
		m.dropSession(id)
		text := "agent rebase failed — " + msg.problem + ": the branch is still on its old base, so its conflicts with " +
			m.baseBranchOf(id) + " remain; read the transcript, then resolve them on the branch"
		if wasHeld && held.Kind == attnGate && !held.Escalated {
			// the card was already waiting on a person (a passed verify
			// whose landing hit the conflict): that decision still
			// stands, and the rebase not happening is the news, not a
			// new stop that replaces it
			m.inbox.put(held)
			m.logPark(id, state.ParkReasonNeedsYou, text)
			if msg.f.Stage == domain.StageVerify {
				// a landing from here would hit the same conflicts, so
				// the decision leads with the rebase again
				if m.landConflicts == nil {
					m.landConflicts = map[domain.FeatureID][]string{}
				}
				m.landConflicts[id] = []string{}
			}
		} else {
			m.raiseEscalation(id, text)
		}
		m.notice = noticeMsg{text: string(id) + ": " + text, isErr: true, id: id}
		return m.loadRows
	}
	f := msg.f
	// The rebase resolved cleanly; re-anchor the recorded fork to main's
	// HEAD so a drifted feature is cleared in the same gesture and the
	// resolution does not go stale under the next rewrite of main.
	if err := m.wt.ReanchorOnMain(context.Background(), &f); err != nil {
		return func() tea.Msg {
			return noticeMsg{text: sanitize(fmt.Sprintf("%s: rebased but fork not re-anchored: %v", f.ID, err)), isErr: true}
		}
	}
	if f.Stage != domain.StageVerify {
		m.notice = noticeMsg{text: string(id) + " rebased onto main"}
		// the base moved: re-measure what its baseline excused (at
		// verify, the re-run below does it as the stage starts)
		return tea.Batch(m.loadRows, m.rebaselineCmd(id))
	}
	return func() tea.Msg {
		m.dropSession(f.ID) // the finished rebase session is stale
		if err := m.engine.Run(f); err != nil {
			return noticeMsg{text: sanitize(err.Error()), isErr: true}
		}
		return noticeMsg{text: string(f.ID) + " rebased onto main → re-verifying", reload: true}
	}
}
