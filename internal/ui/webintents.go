package ui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/engine"
	"github.com/morphis/gummi/internal/state"
	"github.com/morphis/gummi/internal/webapi"
)

// The web face's writes (DESIGN §20.1). Each one is a keypress the page
// makes: it selects the card the way opening its page would, then calls
// what the TUI's key, picker row or menu entry calls — answerDecisionAt,
// routeThreadLine, runCardAction, the card form's own submit — and lets
// webintent.go follow the flow and answer the dialogs it opens.

// Composer is POST /api/cards/{id}/composer: what the composer would do
// with text, by the rules enter routes it with.
func (b *Bridge) Composer(ctx context.Context, id, text string) (webapi.Composer, error) {
	var (
		c  webapi.Composer
		ok bool
	)
	err := b.Do(ctx, func(m *Shell) tea.Cmd {
		r, found := m.rowByID(webID(id))
		if !found {
			return nil
		}
		ok = true
		leave := m.enterCard(r.F.ID, true)
		defer leave()
		c = m.webComposer(r, text)
		return nil
	})
	if err != nil {
		return c, err
	}
	if !ok {
		return c, refuse(WebNotFound, "no card "+id+" on this board")
	}
	return c, nil
}

// Answer is POST /api/cards/{id}/answer: answer the pinned decision with
// one of its options, as person.
//
// The decision must still be the one the page showed — the same ref, or
// it was answered (409 "answered", naming who and what they said when the
// log has it); and the same revision, or the card moved under the page
// (409 "moved").
func (b *Bridge) Answer(ctx context.Context, id string, req webapi.AnswerRequest, person string) (webapi.Card, error) {
	id = string(webID(id))
	release, err := b.answerTurn(ctx, id)
	if err != nil {
		return webapi.Card{}, err
	}
	defer release()
	cur, werr := b.Card(ctx, id)
	if werr != nil {
		return webapi.Card{}, werr
	}
	if cur.Decision == nil || cur.Decision.Ref != req.Ref {
		if req.Ref == "" {
			return webapi.Card{}, refuse(WebBadRequest, "an answer names the decision it answers")
		}
		return webapi.Card{}, b.answeredOrMoved(ctx, id, req.Ref, cur)
	}
	if req.Against != cur.Decision.Against.Token {
		return webapi.Card{}, &WebError{Code: WebConflict, Reason: webapi.ConflictMoved,
			Text: "the card moved since you read it — now " + cur.Decision.Against.Label}
	}
	in := webInput{actor: state.PersonActor(person), confirm: req.Confirm}
	wait := webWait
	if (req.Option == "advance" && cur.Stage == string(domain.StageVerify)) || req.Option == "merge" {
		// landing from the verify gate opens the landing message, which
		// may be waiting on its draft — and only this answer may give it.
		// The words are the message the person read and approved; with
		// none, the landing stops to have its draft read (webdialogs.go).
		in.land = true
		in.message, req.Words = req.Words, ""
		wait = webWaitDraft
	}
	out, werr := b.intent(ctx, webID(id), in, wait, func(m *Shell, r featureRow) (tea.Cmd, error) {
		// read again on the loop: the card may have moved between the
		// check above and now, and the answer is only ever given to the
		// decision (and the answer set) the page showed
		od := m.webOpenDecision(r)
		if od == nil || od.api.Ref != req.Ref {
			return nil, &WebError{Code: WebConflict, Reason: webapi.ConflictAnswered, Text: "someone answered it first"}
		}
		if !strings.HasPrefix(req.Against, od.api.Against.Token) {
			return nil, &WebError{Code: WebConflict, Reason: webapi.ConflictMoved, Text: "the card moved since you read it"}
		}
		return m.webAnswer(r, od, req)
	})
	if werr != nil {
		if we, ok := IsWebError(werr); ok && (we.Reason == webapi.ConflictAnswered || we.Reason == webapi.ConflictMoved) {
			now, cerr := b.Card(ctx, id)
			if cerr != nil {
				return webapi.Card{}, cerr
			}
			return webapi.Card{}, b.answeredOrMoved(ctx, id, req.Ref, now)
		}
		return webapi.Card{}, werr
	}
	if e := out.err(); e != nil {
		return webapi.Card{}, e
	}
	return b.Card(ctx, id)
}

// answeredOrMoved is the 409 for an answer to a decision that is no
// longer the one pinned: "answered" when the log has an answer to that
// very decision, "moved" when the card simply stands somewhere else now
// (a decision that changed kind, a stop nobody answered).
func (b *Bridge) answeredOrMoved(ctx context.Context, id, ref string, cur webapi.Card) error {
	still := cur.Decision != nil && cur.Decision.Ref == ref
	// a stop is answered by moving the card off its stage; one that only
	// changed kind where it stands (idle, then a gate) was answered by
	// nobody — it moved
	if parts := strings.SplitN(ref, ":", 3); len(parts) == 3 && parts[0] != "ask" && parts[2] == cur.Stage {
		still = true
	}
	if by, receipt, ok := b.shell.answerTo(ctx, webID(id), ref); ok && !still {
		text := "someone answered it first"
		if receipt != "" {
			text = receipt
		}
		return &WebError{Code: WebConflict, Reason: webapi.ConflictAnswered, Text: text, By: by, Receipt: receipt}
	}
	now := "nothing is waiting on you"
	switch {
	case still:
		now = cur.Decision.Against.Label
	case cur.Decision != nil:
		now = cur.Decision.Question
	}
	return &WebError{Code: WebConflict, Reason: webapi.ConflictMoved, Text: "the card moved since you read it — now " + now}
}

// webAnswer runs one option of an open decision: the chip's go or keep,
// an ask's option (or its chat row, with the words as the answer), or a
// stop's workflow answer — with words, the line the TUI's composer would
// have carried to the word-eating option.
func (m *Shell) webAnswer(r featureRow, od *webOpenDecision, req webapi.AnswerRequest) (tea.Cmd, error) {
	words := strings.TrimSpace(req.Words)
	switch od.api.Kind {
	case webapi.DecisionConfirm:
		switch req.Option {
		case webOptionGo:
			if !od.chip.goOnEnter && !req.Confirm {
				// the TUI's enter says "that spends credits — press y";
				// the page asks the same question and sends confirm
				return nil, &WebError{Code: WebConflict, Reason: string(webapi.ActionNeedsConfirm), Needs: string(webapi.ActionNeedsConfirm),
					Text: string(r.F.ID) + ": that spends credits — go?"}
			}
			return m.takeReading(r), nil
		case webOptionKeep:
			return m.declineReading(r), nil
		}
		return nil, refuse(WebBadRequest, "the chip answers go or keep, not "+strconv.Quote(req.Option))
	case webapi.DecisionAsk:
		if req.Option == webOptionChat {
			if words == "" {
				return nil, refuse(WebBadRequest, "chatting about it answers with your words — send some")
			}
			if why := tooLong(m.threadInput.CharLimit, "that answer", words); why != "" {
				return nil, refuse(WebBadRequest, why)
			}
			m.threadInput.SetValue(words)
			m.intent.resetComposer = true
			return m.answerAskWith(r, words), nil
		}
		if words != "" {
			return nil, refuse(WebBadRequest, "an option is its own answer — send words with the chat option")
		}
		cursor, picked, err := askPicks(od, req.Option)
		if err != nil {
			return nil, refuse(WebBadRequest, err.Error())
		}
		return m.answerDecisionAt(r, od.d, cursor, picked), nil
	}
	i, ok := od.index[req.Option]
	if !ok {
		return nil, refuse(WebBadRequest, "no option "+strconv.Quote(req.Option)+" on this decision")
	}
	if words != "" {
		if i != od.d.wordConsumer() {
			return nil, refuse(WebBadRequest, "“"+od.d.actions[i].label+"” takes no words")
		}
		if parseInput(words).Kind != verbNone {
			// words carried with an answer are prose for it; a "/verb"
			// is a command, which the composer sends as one
			return nil, refuse(WebBadRequest, "those words are a command — send it from the composer, not with an answer")
		}
		// the line and the decision are one control (DESIGN §6.3): a line
		// typed at a stop is read, and carried by the word-eating answer
		if why := tooLong(m.threadInput.CharLimit, "that line", words); why != "" {
			return nil, refuse(WebBadRequest, why)
		}
		m.threadInput.SetValue(words)
		m.intent.resetComposer = true
		return m.routeThreadLine(r, words, func() *threadDecision { return od.d }), nil
	}
	if a := od.d.actions[i]; a.key == "" && i == od.d.wordConsumer() {
		// a row that IS the composer's words (a send-back with no key of
		// its own, "open a bug from this"): picked bare, the TUI says what
		// it wants rather than doing anything, and so does this
		return nil, &WebError{Code: WebConflict, Reason: webapi.ConflictNeeds, Needs: string(webapi.ActionNeedsMessage),
			Text: "say what is wrong — your words go with “" + a.label + "”"}
	}
	m.clearTransientNotice()
	m.endChat(r.F.ID) // a row picked is the way out of a conversation
	return m.answerDecisionAt(r, od.d, i, nil), nil
}

// askPicks reads an ask's option ids: one index, or — for a multi-pick
// ask — several, comma-separated.
func askPicks(od *webOpenDecision, option string) (int, map[int]bool, error) {
	ids := strings.Split(option, ",")
	picked := map[int]bool{}
	cursor := -1
	for _, raw := range ids {
		i, ok := od.index[strings.TrimSpace(raw)]
		if !ok || i >= len(od.d.ask.Options) {
			return 0, nil, fmt.Errorf("no option %q on this question", raw)
		}
		if cursor < 0 {
			cursor = i
		}
		picked[i] = true
	}
	if len(picked) > 1 && !od.d.ask.MultiPick {
		return 0, nil, fmt.Errorf("this question takes one answer")
	}
	return cursor, picked, nil
}

// Send is POST /api/cards/{id}/send: one composer line, routed as enter
// routes it. The route is reported; a line the TUI would hand to its
// menu is not run (route "menu"), and a turn the agent refused because
// it is mid-turn comes back as 409 "busy" with the line.
func (b *Bridge) Send(ctx context.Context, id string, req webapi.SendRequest, person string) (webapi.SendResponse, error) {
	id = string(webID(id))
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return webapi.SendResponse{}, refuse(WebBadRequest, "an empty line sends nothing")
	}
	release, err := b.answerTurn(ctx, id)
	if err != nil {
		return webapi.SendResponse{}, err
	}
	defer release()
	if err := b.checkAgainst(ctx, id, req.Against); err != nil {
		return webapi.SendResponse{}, err
	}
	var route webapi.Route
	out, werr := b.intent(ctx, webID(id), webInput{actor: state.PersonActor(person)}, webWait,
		func(m *Shell, r featureRow) (tea.Cmd, error) {
			if err := m.checkAgainstOnLoop(r, req.Against); err != nil {
				return nil, err
			}
			decide := func() *threadDecision { return m.openDecision(r) }
			route, _ = m.webLineRoute(r, text, m.classifyThreadLine(r, text, decide))
			if route == webapi.RouteMenu {
				return nil, nil
			}
			if why := tooLong(m.threadInput.CharLimit, "that line", text); why != "" {
				return nil, refuse(WebBadRequest, why)
			}
			m.threadInput.SetValue(text)
			m.intent.resetComposer = true
			return m.routeThreadLine(r, text, decide), nil
		})
	if werr != nil {
		return webapi.SendResponse{}, werr
	}
	if e := out.err(); e != nil {
		return webapi.SendResponse{}, e
	}
	card, werr := b.Card(ctx, id)
	if werr != nil {
		return webapi.SendResponse{}, werr
	}
	return webapi.SendResponse{Route: route, Card: card}, nil
}

// Action is POST /api/cards/{id}/actions/{action}: one entry of the
// card's menu, run as the menu runs it, with the request's values
// answering the dialogs it opens. A nil card with no error means the
// action removed the card (delete).
func (b *Bridge) Action(ctx context.Context, id, action string, req webapi.ActionRequest, person string) (*webapi.Card, error) {
	id = string(webID(id))
	in := webInput{
		actor: state.PersonActor(person), message: req.Message, number: req.Number,
		profile: req.Profile, repo: req.Repo, mode: req.Mode, confirm: req.Confirm,
		land: action == "merge" || action == "squash", autopilot: action == "gate" || action == "autopilot",
	}
	switch req.Mode {
	case "", domain.GateAutopilot, domain.GateAttended:
	default:
		return nil, refuse(WebBadRequest, "mode is autopilot or attended, not "+strconv.Quote(req.Mode))
	}
	release, err := b.answerTurn(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := b.checkAgainst(ctx, id, req.Against); err != nil {
		return nil, err
	}
	wait := webWait
	if (action == "merge" || action == "squash" || action == "advance") && strings.TrimSpace(req.Message) == "" {
		wait = webWaitDraft
	}
	out, werr := b.intent(ctx, webID(id), in, wait, func(m *Shell, r featureRow) (tea.Cmd, error) {
		if err := m.checkAgainstOnLoop(r, req.Against); err != nil {
			return nil, err
		}
		if action == "advance" && r.F.Stage == domain.StageVerify {
			// the menu's next stage at verify is the landing (the TUI's
			// g there opens the landing message): it answers that dialog
			// as a landing does, and stops to have the draft read
			m.intent.in.land = true
		}
		return m.webAction(r, action, req)
	})
	if werr != nil {
		return nil, werr
	}
	if e := out.err(); e != nil {
		return nil, e
	}
	card, werr := b.Card(ctx, id)
	if werr != nil {
		if we, ok := IsWebError(werr); ok && we.Code == WebNotFound && action == "delete" {
			return nil, nil
		}
		return nil, werr
	}
	return &card, nil
}

// checkAgainst refuses a write sent against a pinned decision the card
// has moved past (409 "moved"): the page sends the token it showed, and a
// line or an action meant for that stop is not run at another. An empty
// token checks nothing — a caller with no decision on screen.
func (b *Bridge) checkAgainst(ctx context.Context, id, against string) error {
	if against == "" {
		return nil
	}
	cur, err := b.Card(ctx, id)
	if err != nil {
		return err
	}
	if cur.Decision == nil || cur.Decision.Against.Token != against {
		now := "nothing is waiting on you"
		if cur.Decision != nil {
			now = cur.Decision.Against.Label
		}
		return &WebError{Code: WebConflict, Reason: webapi.ConflictMoved, Text: "the card moved since you read it — now " + now}
	}
	return nil
}

// checkAgainstOnLoop is checkAgainst's second half, on the loop itself:
// the stop and its answer set are the ones the page was shown (the
// revision half, a commit, is what checkAgainst read off the loop).
func (m *Shell) checkAgainstOnLoop(r featureRow, against string) error {
	if against == "" {
		return nil
	}
	od := m.webOpenDecision(r)
	if od == nil || !strings.HasPrefix(against, od.api.Against.Token) {
		return &WebError{Code: WebConflict, Reason: webapi.ConflictMoved, Text: "the card moved since you read it"}
	}
	return nil
}

// webAction runs one menu entry. It must be on the card's menu right now
// — the same list the TUI's ↑ and "/" offer, which is also what withholds
// a verb from a card something else is driving.
func (m *Shell) webAction(r featureRow, id string, req webapi.ActionRequest) (tea.Cmd, error) {
	var entry *webapi.Action
	acts := m.webActions(r)
	for i := range acts {
		if acts[i].ID == id {
			entry = &acts[i]
			break
		}
	}
	if entry == nil {
		return nil, refuse(WebConflict, id+" is not offered on "+string(r.F.ID)+" right now")
	}
	msg := strings.TrimSpace(req.Message)
	m.clearTransientNotice()
	switch id {
	case "deps":
		want := make([]domain.FeatureID, 0, len(req.Cards))
		for _, c := range req.Cards {
			want = append(want, domain.FeatureID(strings.TrimSpace(c)))
		}
		return m.setDependencies(r.F, want), nil
	case "profile":
		if !slices.ContainsFunc(entry.Choices, func(c webapi.Choice) bool { return c.Value == req.Profile }) {
			return nil, refuse(WebBadRequest, "profile "+strconv.Quote(req.Profile)+" is not one "+string(r.F.ID)+" can switch to")
		}
		return m.confirmCardProfileChange(r.F.ID, req.Profile), nil
	case "gate":
		if m.intent.in.mode == "" {
			// the menu entry names the move from where the card is
			m.intent.in.mode = entry.Default
		}
	case "run":
		if msg != "" {
			// a run with a note: the kickoff carries it, as the re-run
			// "send it back" row's words do
			return m.runStageWithNote(r.F, msg), nil
		}
	case "bounce":
		if msg != "" {
			return m.fixedSendBack(r, "bounce", msg), nil
		}
	case "changes", "newbug":
		if msg == "" {
			return nil, &WebError{Code: WebConflict, Reason: webapi.ConflictNeeds, Needs: string(webapi.ActionNeedsMessage),
				Text: "say what is wrong — your words go with it"}
		}
		return m.fixedSendBack(r, id, msg), nil
	case "merge", "squash":
		// the message answers the landing dialog; the menu's own route
		// runs every pre-check the dialog's opening runs
	}
	return m.runCardAction(cardAction{id: entry.ID, key: entry.Key, label: entry.Label, why: entry.Detail, danger: entry.Danger}), nil
}

// setDependencies makes want the card's dependency set, edge by edge
// through the dependency picker's own add and remove (deppicker.go), after
// checking every edge the picker would refuse — a cycle, the card
// itself, a card already coding — so nothing is half-applied.
func (m *Shell) setDependencies(f domain.Feature, want []domain.FeatureID) tea.Cmd {
	store := m.store
	if store == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		dp := &depPicker{}
		if err := dp.buildCands(ctx, store, f); err != nil {
			return noticeMsg{text: sanitize(err.Error()), isErr: true, id: f.ID}
		}
		wanted := map[domain.FeatureID]bool{}
		for _, id := range want {
			wanted[id] = true
		}
		// check first: every wanted card must be addable or attached
		for id := range wanted {
			i := slices.IndexFunc(dp.cands, func(c depCandidate) bool { return c.f.ID == id })
			switch {
			case i < 0 && dp.removeOnly:
				return noticeMsg{text: string(f.ID) + ": " + depReasonLate, isErr: true, id: f.ID}
			case i < 0:
				return noticeMsg{text: string(f.ID) + ": no card " + string(id) + " to depend on", isErr: true, id: f.ID}
			case dp.cands[i].state == candSelf || dp.cands[i].state == candCycle:
				return noticeMsg{text: string(f.ID) + " → " + string(id) + ": " + dp.cands[i].reason, isErr: true, id: f.ID}
			}
		}
		changed := 0
		for i, c := range dp.cands {
			var cmd tea.Cmd
			switch {
			case wanted[c.f.ID] && c.state == candOK:
				dp.cursor = i
				cmd = dp.add(m)
			case !wanted[c.f.ID] && (c.state == candAttached || c.state == candRemove):
				dp.cursor = i
				cmd = dp.remove(m)
			}
			if cmd == nil {
				continue
			}
			changed++
			if n, ok := cmd().(noticeMsg); ok && n.isErr {
				return n
			}
		}
		if changed == 0 {
			return noticeMsg{text: string(f.ID) + ": dependencies unchanged"}
		}
		return noticeMsg{text: string(f.ID) + ": " + dependencyCount(len(wanted)), reload: true}
	}
}

// Resume is POST /api/board/resume: the quit-resume question a headless
// board holds (quitresume.go), answered. Cards picks those back up
// through the dialog's own resume; None is "Not now", which leaves every
// card parked where the quit stopped it. Either way the question is
// settled — as the TUI's dialog is once answered.
func (b *Bridge) Resume(ctx context.Context, req webapi.ResumeRequest, person string) error {
	out, werr := b.intent(ctx, "", webInput{actor: state.PersonActor(person)}, webWait, func(m *Shell, _ featureRow) (tea.Cmd, error) {
		o := m.resumeOffer
		if o == nil {
			return nil, refuse(WebConflict, "there is nothing to pick back up")
		}
		if req.None || len(req.Cards) == 0 {
			m.resumeOffer = nil
			return nil, nil
		}
		picked := o.cards[:0:0]
		for _, id := range req.Cards {
			i := slices.IndexFunc(o.cards, func(c engine.QuitStoppedCard) bool { return string(c.Feature.ID) == id })
			if i < 0 {
				return nil, refuse(WebBadRequest, id+" is not one of the cards the quit stopped")
			}
			picked = append(picked, o.cards[i])
		}
		m.resumeOffer = nil
		return m.resumeQuitStopped(picked), nil
	})
	if werr != nil {
		return werr
	}
	return out.err()
}
