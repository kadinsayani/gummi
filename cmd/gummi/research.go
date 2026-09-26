package main

import (
	"context"
	"fmt"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/driver"
	"github.com/morphis/gummi/internal/state"
)

// runResearchCard is the body behind both `gummi research "<brief>"` and
// `gummi diagnose "<symptom>"`: it mints one RS card and drives it
// headlessly through the decompose gate, streaming the same milestone +
// decision NDJSON as `run`. The two verbs differ only in the card type
// they mint (diagnose adds domain.ModeDiagnosis) and the word their usage
// line calls the argument.
//
// They stay two verbs rather than a `research --diagnose` flag because the
// argument is a different thing — behaviour somebody saw, not a question
// somebody asked — and the usage line is the only place that says so
// before the card exists.
//
// RS has no acceptance-seeded Verification plan and never gets a worktree,
// so neither --acceptance nor the adoption flags are on its surface;
// --until only ever accepts "plan", the sole pre-decompose stop on its
// route.
func runResearchCard(fl cliFlags, args []string, ct domain.CardType, noun string) error {
	verb := ct.Name()
	if len(args) != 1 {
		return fmt.Errorf("%s needs exactly one %s argument", verb, noun)
	}
	brief := args[0]

	opts, err := driverOptions(fl, "")
	if err != nil {
		return err
	}

	return withRunEngine(func(ctx context.Context, d *driver.Driver, _ *state.Store, ws state.Workspace) (driver.Outcome, error) {
		// mint the card first, then take its per-card lock for the drive so
		// this run is the sole governor of the card it just created (two
		// runs mint disjoint cards and so never contend on each other's lock).
		f, err := d.Create(ctx, ct, brief)
		if err != nil {
			return driver.Outcome{}, err
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
