package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/config"
	"github.com/morphis/gummi/internal/domain"
)

// A freeform card is a coding agent that happens to be a card (DESIGN
// §19): no stages, no gates, no critique, no verify — and a worktree, a
// branch, an envelope, a thread and the diff surface, which is every
// structural thing a card has.
//
// This file is its session, and it is deliberately assembled out of the
// two sessions that already exist rather than out of a stage run:
//
//   - From ConsultSession: keyed per card, idempotent to open, and a
//     backend that idles out after 20 minutes and respawns carrying its
//     own transcript. A freeform conversation outlives any one backend.
//   - From BoardSession: folded into an engine.Session so every surface
//     renders it with the same Snapshot machinery, and outside every
//     stage mechanism — no attention-pool slot (the lanes ration
//     contention between autonomous STAGES; a human-paced conversation
//     competes with nothing there), no gate, no verdict, no advance.
//
// What it has that neither of them does is the reason it needed its own
// file: it WRITES. So it takes the card's worktree as its cwd, the card's
// per-card lock for as long as it lives, the card's envelope as a real
// cap, and it checkpoint-commits at the end of every turn — because with
// no stage to hand a tree to, that commit is the only thing between what
// the turn wrote and the branch.
//
// What it deliberately does NOT have is an attention slot, a gate, a
// verdict, a round cap or a kickoff. The corrective-round cap exists to
// stop an unattended loop from spinning; here the human is the loop, and
// the envelope is the only bound.

// freeformIdleTimeout bounds how long a freeform card's backend stays
// spawned with no turns sent. It is consultIdleTimeout's twin and for the
// same reasons (long enough to read a diff and write comments before the
// next turn, short enough that an abandoned conversation's subprocess
// does not outlive the session). Engine.freeformIdleTimeout is seeded
// from it so a test can shrink it.
const freeformIdleTimeout = 20 * time.Minute

// FreeformSession is one freeform card's whole working life: the agent
// conversation, the worktree it writes in, the lock that keeps a second
// gummi out of it, and the envelope it spends against.
//
// One exists per card for this engine's lifetime (OpenFreeform is
// idempotent per card). The backend it holds is not as long-lived: the
// idle timeout closes it and the next turn respawns one carrying this
// session's own transcript, so sess is swapped rather than fixed and mu
// guards the swap.
type FreeformSession struct {
	engine *Engine
	id     domain.FeatureID
	// rc/backend are resolved once, at OpenFreeform, and reused by every
	// respawn — a card's profile does not change mid-conversation.
	rc      config.RoleConfig
	backend string

	mu   sync.Mutex
	sess *Session

	// release drops this engine's hold on the card's per-card lock. Taken
	// once at OpenFreeform and dropped by Close (or Engine.Close), because
	// the hold has to span the whole conversation, not one turn: between
	// two turns the worktree holds uncommitted work and the branch holds
	// commits nothing has reviewed, and a `gummi merge` or a second board
	// arriving in that window is exactly what the lock exists to exclude.
	releaseOnce sync.Once
	release     func()

	idleMu    sync.Mutex
	idleTimer *time.Timer
}

// OpenFreeform starts (or reuses) a freeform card's session, taking the
// card's per-card lock and ensuring its worktree on the way. Every later
// call — another turn, the diff surface delivering comments, reopening
// the card page — returns the identical *FreeformSession.
//
// It refuses a card that is not freeform: a card in the workflow is
// driven by its stages, and handing one a session with no stage, no
// verdict and no gate would be a second way to run it that skips all
// three.
func (e *Engine) OpenFreeform(ctx context.Context, f domain.Feature) (*FreeformSession, error) {
	if !f.IsFreeform() {
		return nil, fmt.Errorf("%s is a %s card: it runs through its stages, not as a freeform session", f.ID, f.Kind)
	}
	if f.Stage == domain.StageDone {
		return nil, fmt.Errorf("%s is closed; reopen it before sending it a turn", f.ID)
	}
	// Serialized end to end for the reason consultMu exists: spawning a
	// backend is too slow to hold e.mu across, so a check-then-act around
	// a released lock would let two callers for the same card both see
	// "not open yet" and both spawn one — and here that would also mean
	// two writers in one worktree.
	e.freeformMu.Lock()
	defer e.freeformMu.Unlock()

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, errors.New("engine is closed")
	}
	if prior := e.freeform[f.ID]; prior != nil {
		e.mu.Unlock()
		return prior, nil
	}
	e.mu.Unlock()

	rc, backend := e.resolveRole(f.Profile, agent.RoleImplementer)
	ff := &FreeformSession{engine: e, id: f.ID, rc: rc, backend: backend}

	release, err := e.lockCard(f.ID)
	if err != nil {
		return nil, fmt.Errorf("%s is being driven elsewhere: %w", f.ID, err)
	}
	ff.release = release

	if err := ff.spawn(ctx, nil); err != nil {
		ff.releaseLock()
		return nil, err
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		ff.stopBackend()
		ff.releaseLock()
		return nil, errors.New("engine is closed")
	}
	e.freeform[f.ID] = ff
	e.mu.Unlock()
	e.send(Event{Feature: f.ID, Stage: domain.StageOpen, Kind: EventStarted})
	return ff, nil
}

// Freeform looks up a card's freeform session without ever spawning one —
// the read path a render or a delivery uses (the diff surface's request
// changes) to reach whatever exists without opening a backend as a side
// effect of asking.
func (e *Engine) Freeform(id domain.FeatureID) *FreeformSession {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.freeform[id]
}

// spawn starts a fresh backend for ff, seeded with the given transcript
// (nothing on the first open, this session's own history on a respawn).
// It installs the new *Session on ff and starts its pump, but never
// touches e.freeform: the caller decides whether this is a first install
// or a respawn of an already-registered session.
func (ff *FreeformSession) spawn(ctx context.Context, seed []Message) error {
	e := ff.engine
	ag := e.agentFor(ff.backend)
	if ag == nil {
		return fmt.Errorf("no agent configured for %s's freeform session", ff.id)
	}
	f, err := e.feature(ctx, ff.id)
	if err != nil {
		return err
	}
	// The worktree, ensured here: this is what "gets its worktree/branch"
	// means, and it is the same locate() every stage goes through, so the
	// branch is cut the same way and a rewrite of main under it is refused
	// the same way. A freeform card comes back with no artifact path.
	workDir, _, err := e.locate(ctx, f)
	if err != nil {
		return err
	}

	sctx, cancel := context.WithCancel(context.Background())
	sess := &Session{
		Feature:     f,
		Role:        agent.RoleImplementer,
		Interactive: true,
		state:       StateInteractive,
		done:        make(chan struct{}),
		ctx:         sctx,
		cancel:      cancel,
		startedAt:   time.Now(),
	}
	sess.setSpawnInfo(ag.Name(), ff.rc.Model, ag.Capabilities().ClientTools)
	if len(seed) > 0 {
		sess.transcript = append(sess.transcript, seed...)
	}

	// The envelope, as a real cap: recomputed on every respawn from what
	// the card has left, so a long conversation's later backends are
	// capped by what is actually still there rather than by what was left
	// when it opened. This is the one floor a freeform card keeps.
	budget := e.stageBudget(f, sess.rate())
	sess.setBudget(budget)
	e.seedCardSpend(sess)

	hints := e.freeformHints(ctx, f, workDir, budget, ag)

	// gummi's one client tool here is resolve_annotation, so the agent can
	// mark each of the reader's diff comments addressed and the open count
	// burns down live (DESIGN §6.1). It reaches the model by whichever of
	// the two routes the backend supports, exactly as a stage's tools do:
	// natively, or over this card's own inbound MCP endpoint. A freeform
	// card needs no ask_user — the person is in the thread, and a reply is
	// the answer.
	var tools []agent.ToolDef
	var mcpPath string
	var mcpTeardown func()
	if caps := ag.Capabilities(); caps.ClientTools || caps.MCPTools {
		tools = stageTools(domain.StageOpen, flavorStage, nil)
		path, teardown, merr := e.startMCPEndpoint(ctx, f, flavorStage)
		if merr != nil {
			cancel()
			return merr
		}
		mcpPath, mcpTeardown = path, teardown
	}

	agentSess, err := ag.NewSession(ctx, agent.SessionOpts{
		WorkDir:        workDir,
		Role:           agent.RoleImplementer,
		Model:          ff.rc.Model,
		SystemHints:    hints,
		Permission:     e.cfg.Permission,
		MaxCredits:     budget * capHeadroom,
		Tools:          tools,
		OutputTokenMax: ff.rc.OutputTokenMax,
		MCPSockPath:    mcpPath,
		FeatureID:      string(ff.id),
		SkillDirs:      e.skillDirsFor(ag, backendLabel(ff.backend)),
		// No ArtifactPath: there is no document. No ResumePath/ResumeID
		// either — this session's continuity is its transcript, carried
		// into each respawn by ensureBackend, not a backend conversation
		// id gummi stored.
	})
	if err != nil {
		if mcpTeardown != nil {
			mcpTeardown()
		}
		cancel()
		return fmt.Errorf("starting %s's freeform session: %w", ff.id, err)
	}
	sess.setMCPTeardown(mcpTeardown)
	if !sess.attachAgent(agentSess) {
		_ = agentSess.Close()
		if mcpTeardown != nil {
			mcpTeardown()
		}
		cancel()
		return errors.New("engine is closed")
	}
	sess.setState(StateInteractive)
	e.trackAgentPID(ff.id, agentSess)

	ff.mu.Lock()
	ff.sess = sess
	ff.mu.Unlock()

	e.wg.Add(1)
	go func() { defer e.wg.Done(); e.pumpFreeform(ff, sess) }()
	ff.armIdleTimer()
	return nil
}

// freeformHints is the whole system-prompt stack a freeform card's
// session gets (DESIGN §19). It is deliberately short, and what is
// missing from it is the design:
//
//   - No stage contract, because there is no stage. Nothing here tells
//     the agent to converge on a plan, fill a section, or emit a verdict.
//   - No artifact, because a freeform card has none. A stage session is
//     told where its document is and to write its progress into it; this
//     one is told the opposite — the thread is the record.
//   - No gate. A stage session is told what its gate will demand; this
//     one has nothing to earn, and saying so is what keeps it from
//     inventing ceremony nobody asked for.
//
// What it keeps is everything about the machine it is standing in: the
// operator's environment, the repository and its own instructions, the
// worktree boundary, the budget, and how the person will steer it.
func (e *Engine) freeformHints(ctx context.Context, f domain.Feature, workDir string, budget float64, ag agent.Agent) []string {
	var hints []string
	if card := e.environmentCard(); card != "" {
		hints = append(hints, card)
	}
	if mgr, err := e.mgr(ctx, &f); err == nil && mgr != nil {
		if card := e.repoInstructionsCard(mgr.RepoRoot()); card != "" {
			hints = append(hints, card)
		}
		if card := e.repoCard(mgr.RepoRoot()); card != "" {
			hints = append(hints, card)
		}
	}
	hints = append(hints, repoInstructionsPrecedenceFreeform, freeformContractHint(f, workDir))
	if budget > 0 {
		hints = append(hints, budgetHintFreeform(budget))
	}
	// Any diff comments still open — the reader's, from before this
	// backend existed. A respawned session must inherit them, or a
	// conversation that idled out between "request changes" and the fix
	// would lose the request entirely.
	hints = append(hints, e.diffReviewHints(ctx, f.ID, ag.Capabilities().ClientTools)...)
	return hints
}

// feature reads the card's authoritative row, so a respawn sees the
// envelope a top-up raised and the branch state a landing changed rather
// than the copy the session opened with. Without a store (tests, a
// transient engine) there is nothing to read and the caller's copy is all
// there is, which is why this returns an error rather than silently
// handing back a zero Feature.
func (e *Engine) feature(ctx context.Context, id domain.FeatureID) (domain.Feature, error) {
	if e.cfg.Store == nil {
		return domain.Feature{}, fmt.Errorf("no store: cannot resolve %s", id)
	}
	return e.cfg.Store.GetFeature(ctx, id)
}

// ensureBackend returns ff's current backend, respawning one — carrying
// over ff's own accumulated transcript — when the last one has idled out.
// Session.Live() is the predicate that makes the respawn trigger exactly
// when the backend is gone and never while a turn is in flight.
func (ff *FreeformSession) ensureBackend(ctx context.Context) (*Session, error) {
	ff.mu.Lock()
	sess := ff.sess
	ff.mu.Unlock()
	spent := sess != nil && sess.isExhausted()
	if sess != nil && sess.Live() && !spent {
		return sess, nil
	}
	// An exhausted session is respawned rather than refused — but only once
	// the envelope actually has room, since the respawn recomputes its cap
	// from what is left. This is what makes "raise the envelope to carry
	// on" true: the exhaustion latch is per backend, so a topped-up card
	// gets a fresh one instead of a conversation that refuses every turn
	// for the rest of the board's life.
	if spent {
		f, ferr := ff.engine.feature(ctx, ff.id)
		if ferr != nil {
			return nil, ferr
		}
		if ff.engine.stageBudget(f, sess.rate()) <= 0 {
			return nil, fmt.Errorf("%s has spent its envelope of %d credits; raise it to carry on", ff.id, f.Budget.Envelope)
		}
	}
	var seed []Message
	if sess != nil {
		seed = sess.Snapshot().Transcript
	}
	if spent && sess.Live() {
		// A spent backend is still running — exhaustFreeform stops the turn,
		// not the process — so it has to be stopped before its replacement
		// exists. Otherwise its pump outlives the session it was started
		// for, and Engine.Close, which joins every pump, waits forever.
		sess.setState(StateDone)
		sess.stop()
	}
	if err := ff.spawn(ctx, seed); err != nil {
		return nil, err
	}
	ff.mu.Lock()
	sess = ff.sess
	ff.mu.Unlock()
	return sess, nil
}

// Send delivers one turn to the card's freeform session, respawning its
// backend first if the last one idled out.
//
// It is the single entry point for every kind of feedback a freeform card
// takes — a person's prose in the thread, and the diff surface's compiled
// review comments — because they are the same thing to the agent: a turn.
// That is the whole of what replaces a stage's kickoff, its verdict
// grammar and its bounce edges.
func (ff *FreeformSession) Send(ctx context.Context, msg string) error {
	sess, err := ff.ensureBackend(ctx)
	if err != nil {
		return err
	}
	a := sess.agent()
	if a == nil {
		return fmt.Errorf("%s's freeform session has no live agent", ff.id)
	}
	sess.appendUser(msg)
	sess.setBusy(true)
	ff.armIdleTimer()
	ff.engine.send(Event{Feature: ff.id, Stage: domain.StageOpen, Kind: EventUpdated})
	if err := a.Send(ctx, msg); err != nil {
		sess.setError(err)
		ff.engine.send(Event{Feature: ff.id, Stage: domain.StageOpen, Kind: EventError, Err: err})
		return err
	}
	return nil
}

// Kickoff sends the card's brief as the session's first turn, and does
// nothing at all if the conversation has already started.
//
// It exists because a freeform card's description is the task: the person
// typed it into the creation dialog and expects work to begin, not to have
// to retype it into the thread. The brief is also in the session's system
// prompt (freeformContractHint), which is what orients a RESPAWNED backend
// — this is the turn that actually starts the first one.
//
// Idempotent by the transcript rather than by a flag, so every path that
// might start a card (creating it, opening its page, delivering the first
// diff comments to a card nobody ever talked to) can call it without
// checking first, and only the first one costs anything.
func (ff *FreeformSession) Kickoff(ctx context.Context) error {
	ff.mu.Lock()
	sess := ff.sess
	ff.mu.Unlock()
	if sess != nil && len(sess.Snapshot().Transcript) > 0 {
		return nil
	}
	f, err := ff.engine.feature(ctx, ff.id)
	if err != nil {
		return err
	}
	brief := strings.TrimSpace(f.Title + "\n\n" + f.OneLiner)
	if brief == "" {
		return nil
	}
	return ff.Send(ctx, brief)
}

// InterruptFreeform stops a freeform card's turn in flight. It is
// Engine.Interrupt's counterpart for a session that is not in e.live, and
// it keeps the backend: the conversation continues, this turn does not.
//
// The checkpoint afterwards is the point. An interrupted turn has still
// written whatever it wrote before being stopped, and on a freeform card
// nothing else will commit it — the EventIdle arm that normally does may
// never arrive for a turn the backend abandoned.
func (e *Engine) InterruptFreeform(ctx context.Context, id domain.FeatureID) error {
	ff := e.Freeform(id)
	if ff == nil {
		return fmt.Errorf("%s has no freeform session to interrupt", id)
	}
	ff.mu.Lock()
	sess := ff.sess
	ff.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("%s has no live turn", id)
	}
	if a := sess.agent(); a != nil {
		if err := a.Interrupt(ctx); err != nil {
			return err
		}
	}
	sess.setBusy(false)
	if err := e.checkpoint(sess); err != nil {
		sess.appendActivity("the worktree is gone — nothing the interrupted turn wrote could be committed: " + err.Error())
	}
	sess.appendActivity("stopped mid-turn by the reader")
	e.send(Event{Feature: id, Stage: domain.StageOpen, Kind: EventUpdated})
	return nil
}

// Snapshot returns a render-safe copy of the session's current backend
// state (an empty Snapshot if none has ever spawned).
func (ff *FreeformSession) Snapshot() Snapshot {
	ff.mu.Lock()
	sess := ff.sess
	ff.mu.Unlock()
	if sess == nil {
		return Snapshot{}
	}
	return sess.Snapshot()
}

// Close ends the card's freeform session: it stops the current backend,
// cancels the idle timer, drops the card lock, and clears the engine's
// reference so a later OpenFreeform starts fresh.
//
// It checkpoints first. A person closing the card page, quitting the
// board, or landing the card must not be the moment work is lost, and
// what the last turn left in the worktree is only on the branch once this
// commit exists.
func (ff *FreeformSession) Close() error {
	ff.settle()
	ff.stopBackend()
	ff.engine.mu.Lock()
	if ff.engine.freeform[ff.id] == ff {
		delete(ff.engine.freeform, ff.id)
	}
	ff.engine.mu.Unlock()
	ff.releaseLock()
	ff.engine.send(Event{Feature: ff.id, Stage: domain.StageOpen, Kind: EventStopped})
	return nil
}

// settle commits whatever is in the card's worktree, so nothing a turn
// wrote is left only on disk. Close does this on its way out and
// Engine.Close calls it directly — the two teardown paths differ in what
// else they tidy, not in whether the work survives.
func (ff *FreeformSession) settle() {
	ff.mu.Lock()
	sess := ff.sess
	ff.mu.Unlock()
	if sess == nil {
		return
	}
	_ = ff.engine.checkpoint(sess)
}

// stopBackend stops ff's current backend and cancels its idle timer,
// without touching e.freeform or the card lock — Engine.Close's
// counterpart to Close, for the caller that has already cleared the map
// itself.
func (ff *FreeformSession) stopBackend() {
	ff.idleMu.Lock()
	if ff.idleTimer != nil {
		ff.idleTimer.Stop()
		ff.idleTimer = nil
	}
	ff.idleMu.Unlock()
	ff.mu.Lock()
	sess := ff.sess
	ff.mu.Unlock()
	if sess != nil {
		sess.stop()
	}
}

// releaseLock drops the card lock exactly once, however many teardown
// paths reach it (Close, Engine.Close, a failed spawn).
func (ff *FreeformSession) releaseLock() {
	ff.releaseOnce.Do(func() {
		if ff.release != nil {
			ff.release()
		}
	})
}

// armIdleTimer (re)starts the idle-close timer, called on spawn and on
// every turn sent or reply landing.
func (ff *FreeformSession) armIdleTimer() {
	d := ff.engine.freeformIdleTimeout
	if d <= 0 {
		return
	}
	ff.idleMu.Lock()
	defer ff.idleMu.Unlock()
	if ff.idleTimer != nil {
		ff.idleTimer.Stop()
	}
	ff.idleTimer = time.AfterFunc(d, ff.onIdleTimeout)
}

// onIdleTimeout closes only the current backend, marking it StateDone
// first so Session.Live() reads false and the next Send respawns. The
// transcript is untouched, and the card lock is kept: the worktree and
// branch are still this session's, and nothing else may drive the card
// just because its backend went to sleep.
func (ff *FreeformSession) onIdleTimeout() {
	ff.mu.Lock()
	sess := ff.sess
	ff.mu.Unlock()
	if sess == nil {
		return
	}
	sess.setState(StateDone)
	sess.stop()
}

// pumpFreeform relays one freeform backend's agent events into
// handleFreeform and exits when that backend stops or its event channel
// closes — pumpConsult's shape, parametrized by the specific *Session
// this goroutine was started for, since ff.sess can move on to a
// respawned one while this pump is still draining the old backend's
// final events.
func (e *Engine) pumpFreeform(ff *FreeformSession, sess *Session) {
	events := sess.agent().Events()
	for {
		select {
		case <-sess.done:
			return
		case ev, ok := <-events:
			if !ok {
				if !sess.finalizedState() {
					sess.setError(errSessionDied)
					e.send(Event{Feature: ff.id, Kind: EventError, Err: errSessionDied})
					sess.stop()
				}
				return
			}
			e.handleFreeform(ff, sess, ev)
		}
	}
}

// handleFreeform folds one backend event into the freeform session —
// handleConsult's shape, plus the two arms a session that writes and
// spends needs: the checkpoint commit when a turn completes, and the
// envelope.
func (e *Engine) handleFreeform(ff *FreeformSession, sess *Session, ev agent.Event) {
	switch ev.Kind {
	case agent.EventTextDelta:
		sess.appendDelta(ev.Text)
	case agent.EventMessage:
		sess.finishAssistant(ev.Text)
	case agent.EventToolCall:
		sess.appendToolCall(ev.CallID, toolLine(ev), ev.Tool, ev.Detail)
	case agent.EventToolResult:
		if ev.Result != nil {
			sess.resolveToolResult(ev.CallID, ev.Result.OK, ev.Result.Output)
		}
	case agent.EventClientToolCall:
		e.dispatchFreeformClientTool(ff, sess, ev.ToolCall)
		return
	case agent.EventContext:
		sess.setContext(ev.Context)
	case agent.EventUsage:
		sess.addSpend(ev.Usage)
		e.recordUsage(sess, ff.id, domain.StageOpen, agent.RoleImplementer, ev.Usage)
		if sess.overBudget() {
			e.exhaustFreeform(ff, sess)
		}
	case agent.EventIdle:
		// Commit what the turn wrote, and do it BEFORE clearing the busy
		// flag. This is the freeform card's whole durability story: there is
		// no stage completion to settle and no gate to hold the work, so a
		// turn that ends uncommitted is a turn whose work exists only in a
		// working tree.
		//
		// The order is deliberately the opposite of a stage's (see the
		// EventIdle arm of Engine.handle): a stage clears busy first so the
		// footer stops claiming an attention slot the agent has already left,
		// and its checkpoint is bookkeeping after the fact. A freeform card
		// holds no slot, and the first thing the person does when it stops is
		// read its diff — so "not busy" here has to mean "committed", or that
		// read lands on a tree whose commit has not happened yet.
		if err := e.checkpoint(sess); err != nil {
			sess.appendActivity("the worktree is gone — nothing this turn wrote could be committed: " + err.Error())
		}
		sess.setBusy(false)
		ff.armIdleTimer() // a reply landing resets the idle clock
	case agent.EventError:
		sess.setError(ev.Err)
	case agent.EventBudgetExhausted:
		// The BACKEND's own cap, not gummi's envelope (handleBoard's
		// identical case has the full reasoning). Nothing gummi can raise
		// answers this one.
		sess.appendSystem("the backend reported its credit cap was reached — " +
			"this conversation cannot continue until the cap is raised on its side")
		sess.setBusy(false)
	default:
		return
	}
	e.send(Event{Feature: ff.id, Stage: domain.StageOpen, Kind: EventUpdated})
}

// exhaustFreeform is the envelope running out on a freeform card. It
// commits what the card has and says what would unblock it — and that is
// all it does, because there is nothing here to park: no stage to leave
// mid-flight, no gate to raise, no lane slot to free. The conversation
// simply refuses further turns (Send checks Exhausted) until the person
// raises the envelope.
func (e *Engine) exhaustFreeform(ff *FreeformSession, sess *Session) {
	if !sess.markExhausted() {
		return
	}
	_ = e.checkpoint(sess)
	sess.appendSystem("this card has spent its envelope — its work is committed to its branch; " +
		"raise the envelope to carry on")
	sess.setBusy(false)
	e.send(Event{Feature: ff.id, Stage: domain.StageOpen, Kind: EventExhausted})
}

// dispatchFreeformClientTool answers a freeform session's client-tool
// call by routing it through the same handleClientTool every stage
// session uses — so resolve_annotation behaves identically here, which is
// the point: the review loop on a freeform card is the one that already
// exists, not a second implementation of it.
//
// Inline on the pump goroutine, unlike dispatchConsultClientTool, which
// hands its call to a goroutine because card_diff shells out to git and
// would stall every event behind it. The only tool reachable here is
// resolve_annotation — one store write, and it resolves itself
// immediately — so it is dispatched the way a stage session's is.
func (e *Engine) dispatchFreeformClientTool(ff *FreeformSession, sess *Session, tc *agent.ToolCall) {
	if tc == nil {
		return
	}
	sess.appendToolCall(tc.ID, tc.Name, tc.Name, "")
	e.handleClientTool(sess, tc)
	e.send(Event{Feature: ff.id, Stage: domain.StageOpen, Kind: EventUpdated})
}
