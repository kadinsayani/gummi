package main

import (
	"context"

	"github.com/morphis/gummi/internal/driver"
	"github.com/morphis/gummi/internal/state"
)

// runVerify implements `gummi verify <id|ref>`: the cheap re-attach for a
// feature whose verify already passed but whose card lost its finalize to a
// crash in the tail (parked at verify, verified:false). It re-runs the
// spec's gummi-checks on the existing branch and, if they pass, finalizes
// the verify gate (stamping verified + reporting the branch ready to land)
// with no fresh agent verify pass — streaming the same NDJSON and exiting
// with the same typed status as a `done` run. It does not create work: a
// genuinely unfinished verify should go through `resume` instead, and
// verify says so (exit 1) when a cheap re-attach cannot be trusted.
func runVerify(args []string) error {
	idArg, err := oneID("verify", args)
	if err != nil {
		return err
	}
	return withRunEngine(func(ctx context.Context, d *driver.Driver, store *state.Store, ws state.Workspace) (driver.Outcome, error) {
		f, err := resolveFeatureID(ctx, store, idArg)
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
		return d.Verify(ctx, f.ID)
	}, driver.Options{})
}
