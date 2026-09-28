package engine

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/spec"
	"github.com/morphis/gummi/internal/state"
	"github.com/morphis/gummi/internal/verify"
	"github.com/morphis/gummi/internal/worktree"
)

// An excusal is a claim about one commit.
//
// The approval baseline runs a card's gummi-checks on its fresh branch,
// and a check already failing there is written off at verify as "FAIL
// (pre-existing)" — the card did not break it. That is only true of the
// base it was measured on. A card cut from main before another card
// landed the package it builds on fails build, test and lint at approval;
// once that card lands and this one is rebased, those checks pass on the
// new base — and the excusal, never re-measured, went on waving through
// whatever the card itself broke, for the rest of its life.
//
// So every baseline row records the commit it was measured on, and when a
// card's fork point is no longer that commit — a rebase, a restack, a
// goal's catch-up — the excused checks are measured again, on the new
// base, before anything is written off against it.

// RebaselineResult says what re-measuring a card's baseline found.
type RebaselineResult struct {
	// Moved is whether the baseline was re-measured at all: the card had
	// excused checks and its base is no longer the commit they were
	// measured on.
	Moved bool
	// From and To are the commits the old and new baseline were measured
	// on (From is "" for a baseline that predates the record).
	From, To string
	// Results are the re-measured checks, on To.
	Results []verify.Result
}

// Cleared names the checks the old baseline excused and the new one
// does not: they are gated at verify again.
func (r RebaselineResult) Cleared(before []state.CheckResult) []string {
	now := map[string]bool{}
	for _, x := range r.Results {
		now[x.Name] = x.OK
	}
	var out []string
	for _, b := range before {
		if !b.OK && now[b.Name] {
			out = append(out, b.Name)
		}
	}
	return out
}

// RebaselineIfBaseMoved re-measures a card's excused checks on its
// current base when the base moved since they were measured. It is a
// no-op — (RebaselineResult{}, nil) — for a card that excuses nothing, a
// card whose base is unchanged or unknown, guarded mode (which never
// auto-runs artifact commands), and every kind without a gummi-checks
// block of its own.
//
// It runs the checks in a throwaway detached checkout of the base commit,
// never in the card's worktree: the question is what the BASE does, and
// the worktree holds the card's own changes by now.
func (e *Engine) RebaselineIfBaseMoved(ctx context.Context, f domain.Feature) (RebaselineResult, error) {
	if e.cfg.Permission == agent.PermissionGuarded || !checksKind(f) || e.cfg.Store == nil {
		return RebaselineResult{}, nil
	}
	rows, err := e.cfg.Store.CheckBaseline(ctx, f.ID)
	if err != nil || len(state.ExcusedChecks(rows)) == 0 {
		return RebaselineResult{}, err
	}
	mgr, err := e.mgr(ctx, &f)
	if err != nil || mgr == nil {
		return RebaselineResult{}, err
	}
	fork, err := mgr.ForkPoint(ctx, &f)
	if err != nil || fork == "" {
		return RebaselineResult{}, err
	}
	from := ""
	moved := false
	for _, r := range rows {
		if r.BaseRev != fork {
			moved = true
			from = r.BaseRev
		}
	}
	if !moved {
		return RebaselineResult{}, nil
	}
	_, specPath, err := e.locate(ctx, f)
	if err != nil {
		return RebaselineResult{}, err
	}
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return RebaselineResult{}, err
	}
	checks, _, err := spec.ParseChecks(string(raw))
	if err != nil || len(checks) == 0 {
		return RebaselineResult{}, err
	}
	checks = baselineEligible(checks)

	parent, err := os.MkdirTemp("", "gummi-rebaseline-")
	if err != nil {
		return RebaselineResult{}, err
	}
	defer func() { _ = os.RemoveAll(parent) }()
	dir := filepath.Join(parent, "base")
	if err := worktree.AddDetached(ctx, mgr.RepoRoot(), dir, fork); err != nil {
		return RebaselineResult{}, err
	}
	defer worktree.RemoveDetached(ctx, mgr.RepoRoot(), dir)
	results, err := verify.RunWithBudget(ctx, dir, checks, verifyStageTimeout)
	if err != nil {
		return RebaselineResult{}, err
	}
	if err := e.cfg.Store.SetCheckBaseline(ctx, f.ID, baselineRows(results, fork, time.Now().UTC())); err != nil {
		return RebaselineResult{}, err
	}
	return RebaselineResult{Moved: true, From: from, To: fork, Results: results}, nil
}

// baselineEligible drops the checks that opted out of the baseline
// (`baseline: false`) — the ones aimed at something the card itself
// builds, which the base cannot be expected to pass.
func baselineEligible(checks []domain.Check) []domain.Check {
	out := make([]domain.Check, 0, len(checks))
	for _, ch := range checks {
		if ch.Baseline != nil && !*ch.Baseline {
			continue
		}
		out = append(out, ch)
	}
	return out
}

// baselineRows is a run's results as the rows the store keeps, each
// naming the commit it was measured on.
func baselineRows(results []verify.Result, base string, at time.Time) []state.CheckResult {
	out := make([]state.CheckResult, 0, len(results))
	for _, r := range results {
		out = append(out, state.CheckResult{
			Name: r.Name, Cmd: r.Cmd, OK: r.OK,
			ExitCode: r.ExitCode, Output: r.Output, RanAt: at, BaseRev: base,
		})
	}
	return out
}

// rebaselineNote re-measures a card's excused checks if its base moved
// and says what that found, in one line for its thread and a notice; ""
// when nothing was re-measured.
func (e *Engine) rebaselineNote(ctx context.Context, f domain.Feature) string {
	var before []state.CheckResult
	if e.cfg.Store != nil {
		before, _ = e.cfg.Store.CheckBaseline(ctx, f.ID)
	}
	res, err := e.RebaselineIfBaseMoved(ctx, f)
	if err != nil {
		return "the excused checks could not be re-measured on the card's new base (" + err.Error() + ") — their excusal stands on the old one"
	}
	if !res.Moved {
		return ""
	}
	return RebaselineSentence(res, before)
}

// RebaselineSentence says what a re-measured baseline changed: which
// excused checks are gated again, and which are still failing on the new
// base too.
func RebaselineSentence(res RebaselineResult, before []state.CheckResult) string {
	var still []string
	for _, r := range res.Results {
		if !r.OK {
			still = append(still, r.Name)
		}
	}
	msg := "re-measured the excused checks on the card's new base " + shortRev(res.To)
	if res.From != "" {
		msg += " (they were measured on " + shortRev(res.From) + ")"
	}
	if cleared := res.Cleared(before); len(cleared) > 0 {
		msg += ": " + joinNames(cleared) + " pass there, so verify gates them again"
	}
	if len(still) > 0 {
		msg += "; still failing on the base itself, so still excused: " + joinNames(still)
	}
	return msg
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	out := names[0]
	for _, n := range names[1 : len(names)-1] {
		out += ", " + n
	}
	return out + " and " + names[len(names)-1]
}

// shortRev is a commit id cut to the length people read.
func shortRev(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}
