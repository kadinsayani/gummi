# gummi: picking the right `resume`

SKILL.md's loop covers the common path: every resumable terminal event
carries a **`next`** field holding the literal `gummi resume …` command for
that stop, with the verb already chosen. Running `next` verbatim is right
almost every time. This file is for when you need to choose yourself.

## Match the flag to why the run stopped

`gummi resume <id>` does different things depending on its flag. The wrong
verb wastes a stage or stalls:

- `--answer "<text>"` — answers a delegated **`question`** event (an
  `ask_user`).
- `--approve` / `--request-changes "<note>"` — decides a **`gate`** event (a
  design gate, which `--gate-approval=attended` hands back to you), and the
  same pair continues a `--until` **`stopped`** run.
- `--bounce [--note "<why>"]` — un-parks a verify-fail (or a review cap-hit)
  **`escalation`**: rewinds the card to its work stage (implement/fix) and
  drives the review → verify tail again. The optional `--note` is an addendum
  to the reborn implement kickoff, alongside any open `%%` diff/spec threads
  the engine already folds in. This is the CLI counterpart of the TUI's `b`
  key; use it when the human has looked at the verify evidence and decided a
  rework is the right call.
- `--envelope N` — clears an **`exhausted`** stop; N must exceed the dry
  envelope (the `envelope` field on the event). It only raises, never lowers.
- `--stage-timeout <duration>` — retries a **`timeout`** with more room. The
  event carries `stage_timeout_used`; if the backend agent was fine and just
  needed longer, double that rather than retrying at the same limit.
- **no decision flag** — re-runs the parked stage exactly as it is. This is
  *only* for retrying a stage that stalled (**`timeout`**), escalated
  (**`escalation`**), or ran dry (after you top up with `--envelope`) — once
  the human has weighed in. A bare `resume` is **not** how you cross a gate or
  answer a question: at a gate it only re-presents the same gate, and at a
  delegated ask it has nothing to send.

Exactly one decision flag per resume (`--answer`, `--approve`,
`--request-changes`, `--bounce` and `--say` are mutually exclusive).
`--envelope` composes with any of them, or stands alone to top up a plain
re-run.

## Before you retry: check `preconditions.check_running`

`exhausted` and `timeout` events carry a `preconditions.check_running` shell
one-liner — run it before following `next`.

gummi ignores SIGHUP so that a detached wrapper's death doesn't kill the model
turn; the flipside is that your wrapper dying (e.g. the harness killed it) can
leave gummi still churning as an orphan. A bare retry there fights the
exclusive lock and looks like a fresh failure.

`check_running` reads that card's own pid file under `.gummi/state/locks/` and
probes it with `kill -0`. If it prints "gummi still running as pid …", wait
until the pid is gone, then follow `next`:

- `gummi watch <id>` follows that run's live agent stream.
- `tail -f .gummi/state/events.jsonl` shows milestones only.

`gummi status <id> --json` also exposes this under `"running"` when you'd
rather branch on JSON than a shell probe.

## A card someone else is driving

**A card the user is driving in the open board is locked, too.** The board
takes the same per-card lock as `run`/`resume`/`merge`/`clean` for every card
it drives, so one of those verbs on such a card exits 1 with "another gummi
process is already driving this card". That is a wait-or-ask, not a fault to
repair: `gummi watch <id>` shows what it is doing, and the lock clears when
that card's session ends. Independent cards are unaffected — the lock is per
card, never per workspace.
