# gummi goals

A **goal** is a card whose work is other cards. Reach for it when the
objective needs several PR-sized pieces that only make sense together; for
one PR-sized piece, `gummi run` is the whole answer and SKILL.md covers it.

## Grammar

```
{{.GoalGrammar}}
```

Every verb and flag in SKILL.md's grammar applies to a goal as well —
`status`, `watch`, `spec`, `merge`, `handoff` and the decision flags all work
on a `GL-NNN` id. The flags above are the ones that *only* apply to a goal.

## How a goal runs

`gummi goal --envelope N "<objective>"` mints one GL goal. It runs the plan
conversation first — the architect agrees the objective, a done-when list of
checkable statements, the limits, and the cards that get there, each serving a
done-when item — and stops at its plan gate like any card (`question`, exit 2,
unless `--gate-approval autopilot --autonomous`). `--plan-file <path>` starts
from a goal doc you wrote.

Approving the plan starts the goal, and from there it runs itself. It creates
its cards, runs each on autopilot on the goal branch `gummi/GL-NNN-slug`, and
lands each one there as one commit. The goal's lead answers the cards'
questions, reads their plans, and re-plans when a card gets stuck. Then it
reviews and verifies the combined branch.

The envelope is the goal's whole budget and a hard ceiling: its cards, its
lead, and its own review and verify all spend inside it.

While it runs, the stream carries `goal` events (what each step did) and
`verified` events for its cards. **A card's `verified` is not the goal's own
`verified`.**

## When it comes back

The run exits `verified` (0) when the goal is ready for you. The event's
`goal` object says how many done-when items were met, whether the goal is
`partial`, which cards landed and which were dropped, and carries the full
report. `gummi status GL-NNN --json` has the same report under `goal`,
including the `decisions_for_review` the lead made on your behalf. Put those
in front of the human. From there:

- `gummi merge GL-NNN` lands the goal on main as one merge commit over its
  cards' commits (`-m` optional — gummi writes it).
- `gummi resume GL-NNN --request-changes "<notes>"` sends it back to its cards
  with the notes; add `--envelope N` to give it more budget.
- `gummi resume GL-NNN --reverse D-N` reverses a decision for review.
- `gummi handoff GL-NNN` closes it without landing; the branch stays.

## Reaching a goal while it runs

These do not drive the goal — each writes one row its lead reads on its next
turn — so they work on a goal whose card lock is held, which is the only state
they are for:

- `--goal-note "<text>"` hands the lead a note.
- `--wrap-up` tells it to finish now: nothing new starts, verified work lands,
  the rest is dropped.
- `--reverse D-N` reverses a decision for review; `--request-changes` adds why.

The substrate levers (`--runs`, `--minutes`, `--retake`) apply to goals that
run experiments: the first two raise the budget (never lower it), and
`--retake` declares an experiment's conclusive runs stale so the goal takes
them again — for when the substrate, not the code, was what failed.
