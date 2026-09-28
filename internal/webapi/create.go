package webapi

// CreateCardRequest is POST /api/cards: the new-card form, every kind.
// Fields that do not apply to Kind are ignored. Answers Card.
type CreateCardRequest struct {
	// Kind is "feature", "bug", "research", "goal" or "freeform".
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Description is the card's brief: a feature's what-and-why, a
	// research card's question, a goal's done-when.
	Description string `json:"description,omitempty"`
	OneLiner    string `json:"oneLiner,omitempty"`
	Profile     string `json:"profile,omitempty"`
	Repo        string `json:"repo,omitempty"`
	// Base is the branch the card forks from and lands on.
	Base string `json:"base,omitempty"`
	// Envelope is the credit budget; nil takes the board's default.
	Envelope *int `json:"envelope,omitempty"`
	// Autopilot starts the card with its gates crossing unattended.
	Autopilot bool `json:"autopilot,omitempty"`
	// Adopt names an existing branch to put the card on (§10 D22); PR
	// names a pull request whose head branch to adopt.
	Adopt string `json:"adopt,omitempty"`
	PR    string `json:"pr,omitempty"`
	// DependsOn lists the cards this one waits for.
	DependsOn []string `json:"dependsOn,omitempty"`
	// StackOn places the card on top of another card's stack.
	StackOn string `json:"stackOn,omitempty"`
	// Bug fields.
	Severity string `json:"severity,omitempty"`
	Repro    string `json:"repro,omitempty"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	Env      string `json:"env,omitempty"`
	// Issue is a GitHub issue number to seed a bug from.
	Issue int `json:"issue,omitempty"`
	// Diagnosis makes a research card a diagnosis rather than a survey.
	Diagnosis bool `json:"diagnosis,omitempty"`
	// Start runs the card's first stage once it is created.
	Start bool `json:"start,omitempty"`
}

// Form is GET /api/form: the choices the new-card form offers.
type Form struct {
	Kinds      []Choice `json:"kinds"`
	Profiles   []string `json:"profiles"`
	Repos      []string `json:"repos"`
	Severities []string `json:"severities"`
	// Branches lists the chosen repo's branches (?repo=), for Base and
	// Adopt.
	Branches []string `json:"branches,omitempty"`
	// Stackable are the cards a new card can be stacked on; Dependable the
	// cards it can depend on.
	Stackable  []CardRef `json:"stackable"`
	Dependable []CardRef `json:"dependable"`
	// Envelope is the board's default envelope.
	Envelope int `json:"envelope"`
}

// Choice is one option in a select: a value and the words for it.
type Choice struct {
	Value  string `json:"value"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// CardRef names a card in a picker.
type CardRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Stage string `json:"stage,omitempty"`
}
