package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/morphis/gummi/internal/domain"
)

// SeedGoal readies a freshly minted goal the way `gummi goal` does before
// its plan conversation starts: continued from after, when one is named, so
// the plan is agreed against what that goal came to know (§17.12); and with
// the owner's reference documents in its notebook, which the architect
// plans against and the plan gate pins (§17.10). Every surface that mints
// a goal calls this rather than doing either half itself.
//
// An error names which half failed as "after <id>: …" or
// "reference <path>: …".
func (e *Engine) SeedGoal(ctx context.Context, goalID, after domain.FeatureID, references []string) error {
	if after != "" {
		if err := e.ContinueGoal(ctx, goalID, after); err != nil {
			return fmt.Errorf("after %s: %w", after, err)
		}
	}
	nb := e.goalNotebook(goalID)
	for _, p := range references {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		if err := nb.AddReference(p); err != nil {
			return fmt.Errorf("reference %s: %w", p, err)
		}
	}
	return nil
}
