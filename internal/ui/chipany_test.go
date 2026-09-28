package ui

// pendingChip is the chip up on any card, for tests that raise one on a
// one-card board and ask whether it is there.
func (m *Shell) pendingChip() *reentryReading {
	for _, p := range m.chips {
		return p
	}
	return nil
}
