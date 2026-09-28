package main

import (
	"context"
	"fmt"
	"os"

	"github.com/morphis/gummi/internal/state"
	"github.com/morphis/gummi/internal/worktree"
)

// runSpec implements `gummi spec <id|ref>` (DESIGN §3): a read-only dump of
// the item's current design artifact (a feature's spec or a bug's report) as
// markdown, wherever it lives right now — its workspace home, its draft, or
// a mid-flight worktree copy. It drives nothing and holds no lock.
func runSpec(args []string) error {
	idArg, err := oneID("spec", args)
	if err != nil {
		return err
	}
	return withReadWorkspace(func(ctx context.Context, store *state.Store, wt *worktree.Pool, ws state.Workspace) error {
		f, err := resolveFeatureID(ctx, store, idArg)
		if err != nil {
			return err
		}
		// A freeform card has no artifact at all (DESIGN §19): its thread is
		// its record. Without this the empty ArtifactPath is joined onto the
		// workspace root and the reader gets "read <root>: is a directory",
		// which is a filesystem accident rather than an answer.
		if f.IsFreeform() {
			return fmt.Errorf("%s is a freeform card: it carries no document — read its diff (`gummi diff %s`) instead", f.ID, f.ID)
		}
		path := artifactPath(wt, ws, &f)
		if path == "" {
			return fmt.Errorf("%s has no spec yet — it is created when the plan stage first runs", f.ID)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(raw)
		return err
	})
}
