package ui

import "github.com/morphis/gummi/internal/threadfold"

// sanitize strips terminal escape sequences and control characters from
// untrusted text (model output, provider error strings) before it is
// rendered — threadfold.Sanitize, which the thread's shared wording
// already applies, so a line the TUI builds itself and a line it takes
// from the fold are cleaned by the same rule.
func sanitize(s string) string { return threadfold.Sanitize(s) }
