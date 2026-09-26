package main

import (
	"strings"
	"testing"
)

// runCLI drives the real cobra tree exactly as main() does: routing, flag
// parsing, and positional handling all included.
//
// Tests used to call the runXxx bodies directly with a hand-built
// []string, which skipped the parser a user actually meets. That blind
// spot is why `gummi stack new --name`, `gummi stack add --pos` and
// `gummi merge --m` could each be declared, documented, covered by a
// passing test — and rejected by the binary. Going through the tree means
// a test asserting a flag works proves the flag exists.
func runCLI(argv ...string) error { return run(argv) }

// mustCLI drives the tree and fails the test if the command errors.
func mustCLI(t *testing.T, argv ...string) {
	t.Helper()
	if err := runCLI(argv...); err != nil {
		t.Fatalf("gummi %v: %v", argv, err)
	}
}

// parsedFlags parses argv against a real command's own flag surface and
// returns the view its body sees, so a test of a body helper exercises the
// flags the binary actually declares rather than a stand-in for them.
func parsedFlags(t *testing.T, path string, argv ...string) cliFlags {
	t.Helper()
	resetFlags(rootCmd)
	t.Cleanup(func() { resetFlags(rootCmd) })
	cmd, _, err := rootCmd.Find(strings.Fields(path))
	if err != nil {
		t.Fatalf("finding %q on the command tree: %v", path, err)
	}
	if err := cmd.Flags().Parse(argv); err != nil {
		t.Fatalf("gummi %s %v: %v", path, argv, err)
	}
	return cmdFlags(cmd)
}
