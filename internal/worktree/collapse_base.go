package worktree

import (
	"context"
	"fmt"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/state"
)

// ResolveCollapseBase resolves the base commit a collapse should reset onto:
// the fork point of f's branch with its parent card's branch when a
// dependency edge names one (FD-058..062), else the fork point with the
// repository's local main. A card with more than one declared parent has no
// single unambiguous base, so it is a hard error rather than an arbitrary
// pick; a named parent whose branch cannot be resolved is likewise a hard
// error rather than a silent fallback to main.
func ResolveCollapseBase(ctx context.Context, store *state.Store, mgr *Manager, f *domain.Feature) (string, error) {
	// A card that sits in a stack, or was cut from a chosen base, forks from
	// the tip of that base and nothing else: collapsing onto main (or onto
	// whatever a dependency edge names) would fold the cards beneath it
	// into this card's one commit. A dependency is scheduling, never the
	// base (DESIGN §18.1), so it does not get a vote here.
	if f.StackID != "" || f.Base != "" {
		// the recorded fork point is exactly what a replay of this card
		// keeps, so it stays right while the base has moved on
		if fp, err := mgr.ForkPoint(ctx, f); err == nil && fp != "" {
			if ok, err := gitOK(ctx, mgr.RepoRoot(), "merge-base", "--is-ancestor", fp, f.BranchName()); err == nil && ok {
				return fp, nil
			}
		}
		tip, err := mgr.BaseHead(ctx, f)
		if err != nil {
			return "", fmt.Errorf("resolving collapse base for %s: base tip: %w", f.ID, err)
		}
		sha, err := runGit(ctx, mgr.RepoRoot(), "merge-base", tip, f.BranchName())
		if err != nil {
			return "", fmt.Errorf("resolving collapse base for %s: fork point with its base: %w", f.ID, err)
		}
		return sha, nil
	}
	deps, err := store.ListDependencies(ctx, f.ID)
	if err != nil {
		return "", fmt.Errorf("resolving collapse base for %s: %w", f.ID, err)
	}
	switch len(deps) {
	case 0:
		sha, err := runGit(ctx, mgr.RepoRoot(), "merge-base", "HEAD", f.BranchName())
		if err != nil {
			return "", fmt.Errorf("resolving collapse base for %s: fork point with main: %w", f.ID, err)
		}
		return sha, nil
	case 1:
		parent, err := store.GetFeature(ctx, deps[0])
		if err != nil {
			return "", fmt.Errorf("resolving collapse base for %s: parent %s: %w", f.ID, deps[0], err)
		}
		if _, err := runGit(ctx, mgr.RepoRoot(), "rev-parse", "--verify", "--quiet", "refs/heads/"+parent.BranchName()); err != nil {
			return "", fmt.Errorf("resolving collapse base for %s: parent %s branch %s: %w", f.ID, parent.ID, parent.BranchName(), err)
		}
		sha, err := runGit(ctx, mgr.RepoRoot(), "merge-base", parent.BranchName(), f.BranchName())
		if err != nil {
			return "", fmt.Errorf("resolving collapse base for %s: fork point with parent %s: %w", f.ID, parent.ID, err)
		}
		return sha, nil
	default:
		return "", fmt.Errorf("cannot resolve collapse base for %s: card has %d dependencies", f.ID, len(deps))
	}
}
