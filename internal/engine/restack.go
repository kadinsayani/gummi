package engine

import (
	"context"
	"slices"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/stack"
	"github.com/morphis/gummi/internal/worktree"
)

// restackMaxTicks caps Restack's walk. The policy moves at most one card
// per tick, so a stack of N settles in N ticks and anything past 4N is a
// member that reports stale forever — a bug, not a long stack.
const restackMaxTicks = 64

// RestackResult is what a whole restack did.
type RestackResult struct {
	Stack domain.StackID
	// Replayed are the cards whose branches moved, in the order they did.
	Replayed []domain.FeatureID
	// Push is the `git push --force-with-lease` each replayed branch now
	// needs to reach its remote. gummi prints it and never runs it (§18.5).
	Push []string
	// Conflict is set when a replay stopped on conflicts: Card's branch is
	// untouched and the walk ended there.
	Conflict     *worktree.RebaseConflictError
	ConflictCard domain.FeatureID
	// Waiting is the policy's reason the walk stopped short without a
	// conflict (a member with a live session), WaitingOn the card.
	Waiting   string
	WaitingOn domain.FeatureID
}

// Restack walks one stack to a fixed point: StackTick until nothing is
// left to replay, a replay conflicts, or the policy says to wait. It is
// the forced walk `gummi stack restack` and the web face ask for; the
// board's own ticks do the same work one step at a time.
func (e *Engine) Restack(ctx context.Context, id domain.StackID) (RestackResult, error) {
	out := RestackResult{Stack: id}
	for range restackMaxTicks {
		res, err := e.StackTick(ctx, id)
		if err != nil {
			return out, err
		}
		if res.Conflict != nil {
			out.Conflict, out.ConflictCard = res.Conflict, res.Restacked
			return out, nil
		}
		if res.Restacked != "" {
			out.Replayed = append(out.Replayed, res.Restacked)
			if res.Push != "" {
				out.Push = append(out.Push, res.Push)
			}
		}
		if w := res.Settled; w != nil {
			// The walk this call settled may have begun on the board's own
			// ticks, before this call took the stack's lock: those replays
			// moved branches too, and this answer is the one place their
			// pushes are handed back now. The walk holds this call's own
			// replays as well, in the order they happened, so it is the
			// whole answer.
			out.Replayed, out.Push = slices.Clone(w.Cards), nil
			for _, p := range w.Push {
				if p != "" {
					out.Push = append(out.Push, p)
				}
			}
		}
		if !res.Again {
			break
		}
		if len(res.Actions) == 1 && res.Actions[0].Kind == stack.ActionWait {
			out.Waiting, out.WaitingOn = res.Actions[0].Reason, res.Actions[0].Card
			break
		}
	}
	return out, nil
}

// PushCommand is the command a person runs to publish a branch gummi
// rewrote, when nothing says where the branch lives: origin, under its own
// name. gummi never pushes; it says what to push.
func PushCommand(branch string) string {
	return PushCommandTo("origin", branch, branch)
}

// PushCommandTo is PushCommand for a branch whose remote is known — the
// one git's own branch config says it tracks, and the name it has there.
func PushCommandTo(remote, branch, remoteBranch string) string {
	if remoteBranch == "" || remoteBranch == branch {
		return "git push --force-with-lease " + remote + " " + branch
	}
	return "git push --force-with-lease " + remote + " " + branch + ":" + remoteBranch
}

// replayPushCommand is the push a replayed card's branch needs: to the
// remote and branch it tracks when it tracks one (someone pushed it with
// -u, or gummi adopted it from a PR), so the line is right to copy for a
// fork or a second remote; to origin under its own name otherwise.
func (e *Engine) replayPushCommand(ctx context.Context, f *domain.Feature) string {
	branch := f.BranchName()
	if branch == "" {
		return ""
	}
	if mgr, err := e.pool.ManagerFor(ctx, f); err == nil {
		if remote, rb, ok := mgr.Upstream(ctx, f); ok {
			return PushCommandTo(remote, branch, rb)
		}
	}
	return PushCommand(branch)
}
