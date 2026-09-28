package webapi

// Card is GET /api/cards/{id}: the card page's head, its pinned decision
// and what else can be done to it.
type Card struct {
	Row
	// Branch is the card's git branch; Base what it forks from and lands
	// on. Adopted marks a branch gummi did not cut (DESIGN §10 D22).
	Branch  string `json:"branch,omitempty"`
	Base    string `json:"base,omitempty"`
	Adopted bool   `json:"adopted,omitempty"`
	// OneLiner is the card's short summary.
	OneLiner string `json:"oneLiner,omitempty"`
	// Decision is the one open decision the page pins, ranked the way the
	// TUI ranks it; nil when nothing is waiting on a person.
	Decision *Decision `json:"decision,omitempty"`
	// DecisionsMore counts the other open decisions behind it.
	DecisionsMore int `json:"decisionsMore"`
	// Actions is the card's menu: every non-decision action the TUI offers
	// on it right now.
	Actions []Action `json:"actions"`
	// Composer says what a line typed into the card's composer would do.
	Composer Composer `json:"composer"`
}

// DecisionKind classifies an open decision.
type DecisionKind string

// The decision kinds. They match the durable decision_open record's kinds,
// plus confirm for a TUI confirmation a web intent has to put to a person.
const (
	DecisionGate     DecisionKind = "gate"
	DecisionAsk      DecisionKind = "ask"
	DecisionVerify   DecisionKind = "verify"
	DecisionConflict DecisionKind = "conflict"
	DecisionBudget   DecisionKind = "budget"
	DecisionIdle     DecisionKind = "idle"
	DecisionConfirm  DecisionKind = "confirm"
	// DecisionFailure is a stage that failed (the session errored, or the
	// backend could not serve it); DecisionClosed a card that has ended,
	// with the answers that remain (a follow-up bug, adopting it back).
	// Neither is a durable record; both are what the TUI's card page pins.
	DecisionFailure DecisionKind = "failure"
	DecisionClosed  DecisionKind = "closed"
)

// Anchor names the tab a decision is about, which the page shows beside it.
type Anchor string

// The anchors.
const (
	AnchorSpec   Anchor = "spec"
	AnchorDiff   Anchor = "diff"
	AnchorThread Anchor = "thread"
)

// Decision is an open decision with its answers. Options are regenerated
// on every read and never stored (DESIGN §10 decision 18).
type Decision struct {
	// Ref identifies the decision: an ask's decision id, or a workflow
	// decision's record id. An answer names it back.
	Ref      string       `json:"ref"`
	Kind     DecisionKind `json:"kind"`
	Question string       `json:"question"`
	// Word is the decision's one-word name as the page heads it ("design
	// gate", "verify passed", "working"): said by the server, which knows
	// the outcome behind a kind, so the page never derives it.
	Word string `json:"word,omitempty"`
	// Tone tints it: "ok", "warn", "err" or "info"; empty takes the
	// stage's own hue (a clean gate).
	Tone   string `json:"tone,omitempty"`
	Anchor Anchor `json:"anchor"`
	// Against is the revision the decision was raised on (§20.1). An
	// answer sends Against.Token back and is refused with 409 if the card
	// has moved since.
	Against Against  `json:"against"`
	Options []Option `json:"options"`
	// Multi marks a question that takes several of its options at once:
	// the answer names them comma-separated ("0,2").
	Multi bool `json:"multi,omitempty"`
}

// Against is a decision's revision: an opaque token for the server, and a
// label for a person ("spec a1b2c3d", "verify run 3 on 9f8e7d6").
type Against struct {
	Token string `json:"token"`
	Label string `json:"label"`
}

// Option is one answer to a decision.
type Option struct {
	// ID is what an answer names; it is stable only within one read. A
	// workflow answer's id is its action ("advance", "bounce"); an ask's
	// option is its index ("0", "1"; several comma-separated for a
	// multi-pick question) and its chat row "chat"; the confirm chip's are
	// "go" and "keep".
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	// Words marks an option that takes a note ("send back with …").
	Words bool `json:"words"`
	// Relabel is the label to show once words have been typed, when the
	// option reads differently with a note than without.
	Relabel string `json:"relabel,omitempty"`
	// Chat marks the "chat about this" answer to an ask: it opens a
	// conversation instead of answering.
	Chat bool `json:"chat,omitempty"`
	// Danger marks an answer that discards or cannot be undone.
	Danger bool `json:"danger,omitempty"`
	// CarriesComments marks an answer that takes the card's unresolved
	// diff comments with it (a send-back from a failed verify).
	CarriesComments bool `json:"carriesComments,omitempty"`
}

// ActionNeeds is the input an action asks for before it runs.
type ActionNeeds string

// The inputs an action can need. The page collects the value and sends it
// in ActionRequest; nothing is asked for in a follow-up dialog.
const (
	ActionNeedsMessage ActionNeeds = "message"
	ActionNeedsNumber  ActionNeeds = "number"
	ActionNeedsProfile ActionNeeds = "profile"
	ActionNeedsCards   ActionNeeds = "cards"
	ActionNeedsConfirm ActionNeeds = "confirm"
	// ActionNeedsRepo: the repository picker (ActionRequest.Repo).
	ActionNeedsRepo ActionNeeds = "repo"
	// ActionNeedsMode: the autopilot switch's mode (ActionRequest.Mode).
	ActionNeedsMode ActionNeeds = "mode"
	// ActionNeedsText: one line of text (ActionRequest.Message), such as a
	// pull request's URL or number; empty is an answer too when Detail
	// says so.
	ActionNeedsText ActionNeeds = "text"
)

// Action is one entry in a card's menu.
type Action struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Key is the TUI's key for it, shown as a hint.
	Key    string `json:"key,omitempty"`
	Danger bool   `json:"danger,omitempty"`
	// Needs names the input the page must collect first; empty runs at once.
	Needs  ActionNeeds `json:"needs,omitempty"`
	Detail string      `json:"detail,omitempty"`
	// Default is the input's suggested value: the landing message the
	// verify gate drafted, the current envelope, the current dependencies
	// (comma-separated ids).
	Default string `json:"default,omitempty"`
	// Choices are the values a profile or repository picker offers.
	Choices []Choice `json:"choices,omitempty"`
}

// Route is where a composer line goes.
type Route string

// The composer routes, in the TUI's own terms.
const (
	RouteSteer    Route = "steer"
	RouteConsult  Route = "consult"
	RouteFreeform Route = "freeform"
	RouteGoalNote Route = "goalnote"
	RouteAnswer   Route = "answer"
	RouteVerb     Route = "verb"
	RouteBlocked  Route = "blocked"
	// RouteMenu: the line names an action the card has but is not one of
	// its answers right now ("/rebase" at a gate). The TUI opens its menu
	// filtered by the word; the page opens the card's actions.
	RouteMenu Route = "menu"
	// RouteRead: a line typed at a stop that no answer takes words for. It
	// is sent as a line, and the board reads it to place it (a routed
	// re-entry, or a message) exactly as the TUI's enter does — it is never
	// the highlighted answer.
	RouteRead Route = "read"
)

// Composer is the card's composer state, and the answer to POST
// /api/cards/{id}/composer (a SendRequest's Text): what sending that line
// would do, asked as a person types. That route changes nothing.
type Composer struct {
	// Says is the line under the composer that tells a person what sending
	// will do ("steers the implementer mid-turn").
	Says  string `json:"says"`
	Route Route  `json:"route"`
}

// AnswerRequest is POST /api/cards/{id}/answer.
type AnswerRequest struct {
	Ref    string `json:"ref"`
	Option string `json:"option"`
	Words  string `json:"words,omitempty"`
	// Against is the Decision.Against.Token the answer was given against.
	Against string `json:"against"`
	// Confirm answers the confirmation the answer's flow raises on the way
	// (the TUI's y), when the page has already asked it: a 409 "needs"
	// with needs "confirm" says what it is.
	Confirm bool `json:"confirm,omitempty"`
}

// Conflict reasons a 409 carries in Error.Error. Any other 409 is a
// refusal, and Error.Error is the board's own sentence for it.
const (
	ConflictMoved    = "moved"
	ConflictAnswered = "answered"
	ConflictBusy     = "busy"
	// ConflictNeeds: the flow stopped at a question the request carried
	// no answer for. Needs names the input, Text the question; ask the
	// person and send the request again with it.
	ConflictNeeds = "needs"
	// ConflictNewCard: the line reads as separate work. Text is the line;
	// the page opens the new-card form seeded with it.
	ConflictNewCard = "newcard"
)

// SendRequest is POST /api/cards/{id}/send: one composer line.
type SendRequest struct {
	Text string `json:"text"`
	// Against, when set, is the pinned decision's Against.Token as the
	// page showed it: a line sent against a card that has moved since is
	// refused with 409 "moved" rather than routed at a stop nobody saw.
	Against string `json:"against,omitempty"`
}

// SendResponse says where the line went and returns the card as it now
// stands.
type SendResponse struct {
	Route Route `json:"route"`
	Card  Card  `json:"card"`
}

// ActionRequest is POST /api/cards/{id}/actions/{action}: the input the
// action's Needs asked for.
type ActionRequest struct {
	Message string   `json:"message,omitempty"`
	Number  *int     `json:"number,omitempty"`
	Profile string   `json:"profile,omitempty"`
	Cards   []string `json:"cards,omitempty"`
	// Confirm is true when the page asked an ActionNeedsConfirm action's question.
	Confirm bool `json:"confirm,omitempty"`
	// Repo is the repository picker's answer.
	Repo string `json:"repo,omitempty"`
	// Mode is the autopilot switch's answer: "autopilot" or "attended".
	// Empty takes the one the card's menu entry names.
	Mode string `json:"mode,omitempty"`
	// Against, when set, is the pinned decision's Against.Token as the
	// page showed it: an action that moves the card (a gate crossing, a
	// landing) is refused with 409 "moved" if the card moved since.
	Against string `json:"against,omitempty"`
}
