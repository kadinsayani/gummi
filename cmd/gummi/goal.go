package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/driver"
	"github.com/morphis/gummi/internal/state"
)

// runGoal implements `gummi goal [flags] "<objective>"`: it mints one goal
// and drives it headlessly — the plan conversation first (a question per
// turn unless --autonomous), then its cards on the goal branch, its review
// and its verify — to the point it is ready for you. Land it with `gummi
// merge`, send it back with `gummi resume --request-changes`, or hand it
// off. The envelope is the goal's whole budget: its cards, its lead and
// its own review and verify all spend inside it.
func runGoal(fl cliFlags, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("goal needs exactly one objective argument")
	}
	objective := args[0]
	doc, err := readAcceptance(fl.String("plan-file"))
	if err != nil {
		return fmt.Errorf("%s", strings.NewReplacer("--acceptance", "--plan-file").Replace(err.Error()))
	}
	// no --repo on the goal's surface: a goal is not in a repository. Its
	// cards name their own in the plan, and the goal's own home is settled
	// from them at the plan gate (DESIGN §17.2).
	opts, err := driverOptions(fl, "")
	if err != nil {
		return err
	}
	opts.GoalDoc = doc

	return withRunEngine(func(ctx context.Context, d *driver.Driver, _ *state.Store, ws state.Workspace) (driver.Outcome, error) {
		f, err := d.Create(ctx, domain.CardType{Kind: domain.KindGoal}, objective)
		if err != nil {
			return driver.Outcome{}, err
		}
		// A goal that continues another starts from what that one came to
		// know, and the owner's reference documents go into its notebook
		// before the plan conversation starts (engine.SeedGoal).
		after := domain.FeatureID(strings.ToUpper(strings.TrimSpace(fl.String("after"))))
		if err := d.SeedGoal(ctx, f.ID, after, strings.Split(fl.String("reference"), ",")); err != nil {
			return driver.Outcome{}, fmt.Errorf("--%w", err)
		}
		release, err := state.AcquireLock(ws.CardLockFile(f.ID))
		if err != nil {
			return driver.Outcome{}, err
		}
		defer release()
		clearPID, err := trackPID(ws, f.ID)
		if err != nil {
			return driver.Outcome{}, err
		}
		defer clearPID()
		return d.Drive(ctx, f)
	}, opts)
}
