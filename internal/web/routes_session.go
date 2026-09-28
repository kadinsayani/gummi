package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/morphis/gummi/internal/state"
	"github.com/morphis/gummi/internal/webapi"
)

func (s *Server) sessionRoutes() {
	s.public("GET /api/session", s.handleSession)
	s.public("POST /api/pair", s.handlePair)
	s.public("POST /api/pair/request", s.handlePairRequest)
	s.public("POST "+adminPath, s.handleAdminPair)
	s.api("POST /api/unpair", s.handleUnpair)
	s.api("GET /api/events", s.handleEvents)
}

// handleSession is GET /api/session. It answers without a cookie: it is
// how the page decides between the pairing form and the board.
//
// A browser that is not paired is told only what the pairing form needs —
// that it is not paired, and whether a code is live. What the board is,
// where it runs and who a code was printed for are for a paired device.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	who, authed, renew := s.identify(r)
	if !authed {
		writeJSON(w, http.StatusOK, unpairedSession{PairingLive: s.opt.Pairing.Live()})
		return
	}
	if renew != "" {
		s.setDeviceCookie(w, r, renew)
	}
	writeJSON(w, http.StatusOK, webapi.Session{
		Authed:     true,
		OpenAccess: s.opt.OpenAccess,
		Version:    s.opt.Version,
		Repo:       s.opt.Repo,
		Host:       s.opt.Host,
		Person:     who.Person,
		Device:     who.Device,
		DeviceID:   who.DeviceID,
	})
}

// unpairedSession is webapi.Session as a browser that is not paired gets
// it: the same field names, and nothing else.
type unpairedSession struct {
	Authed      bool `json:"authed"`
	PairingLive bool `json:"pairingLive"`
}

// maxPersonName bounds the name given when pairing: it is shown beside
// every receipt, so it is a name, not a paragraph.
const maxPersonName = 40

// reservedNames are the actors the board records that are not a named
// person: the terminal's bare "user", the unattended "autopilot", a
// goal's own "goal", and "local", the viewer of a board served without
// pairing. A person named one of them would read as that actor wherever
// the name is shown bare.
var reservedNames = []string{state.ActorUser, state.ActorAutopilot, "goal", openWho.Person}

// personName validates the name a person pairs under.
func personName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	switch {
	case name == "":
		return "", errors.New("say who you are: pairing needs a name")
	case len([]rune(name)) > maxPersonName:
		return "", fmt.Errorf("a name is at most %d characters", maxPersonName)
	case strings.Contains(name, ":"):
		// an actor is "kind:detail" ("user:Simon"); a colon in a name
		// would let it pass for another kind of actor
		return "", errors.New("a name cannot contain a colon")
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return "", errors.New("a name cannot carry control characters")
		}
	}
	for _, reserved := range reservedNames {
		if strings.EqualFold(name, reserved) {
			return "", fmt.Errorf("%q is a name the board uses for itself; pick another", name)
		}
	}
	return name, nil
}

// handlePair is POST /api/pair: redeem the code printed in the server's
// terminal for a device token, under the name of the person pairing.
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if s.opt.OpenAccess {
		writeError(w, http.StatusConflict, "this board is served without pairing")
		return
	}
	if !s.redeems.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many pairing attempts; wait a minute and try again")
		return
	}
	var body webapi.PairRequest
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "expected a JSON body with a code and a name")
		return
	}
	// The name is checked before the code is spent: a typo in a name must
	// not cost a guess. A code minted for a person needs no name.
	person, err := personName(body.Name)
	if err != nil && (strings.TrimSpace(body.Name) != "" || s.opt.Pairing.LiveFor() == "") {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lastLock := s.opt.Pairing.lockedUntilTime()
	named, err := s.opt.Pairing.RedeemFor(strings.TrimSpace(body.Code))
	if err != nil {
		var (
			wrong  *WrongCodeError
			locked *LockedError
		)
		switch {
		case errors.As(err, &wrong):
			s.opt.Log("web: wrong pairing code from %s (%s left)", clientIP(r), tries(wrong.Remaining))
			remaining := wrong.Remaining
			writeJSON(w, http.StatusForbidden, webapi.Error{Error: err.Error(), Remaining: &remaining})
		case errors.Is(err, ErrCodeBurned):
			s.opt.Log("web: pairing code burned by wrong guesses from %s", clientIP(r))
			writeError(w, http.StatusForbidden, err.Error())
		case errors.As(err, &locked):
			if locked.Until != lastLock {
				s.opt.Log("web: pairing is locked until %s after %d wrong guesses (the last from %s); `gummi web pair` unlocks it",
					locked.Until.Local().Format("15:04"), wrongGuessBudget, clientIP(r))
			}
			writeError(w, http.StatusTooManyRequests, err.Error())
		default:
			writeError(w, http.StatusForbidden, err.Error())
		}
		return
	}
	if named != "" {
		person = named
	}
	if person == "" {
		writeError(w, http.StatusBadRequest, "say who you are: pairing needs a name")
		return
	}
	token, dev, err := s.opt.Devices.Pair(person, deviceName(r.UserAgent()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setDeviceCookie(w, r, token)
	s.opt.Log("web: paired %s on %s (%s)", dev.Person, dev.Name, dev.ID)
	writeJSON(w, http.StatusOK, webapi.PairResponse{Person: dev.Person, Device: dev.Name, DeviceID: dev.ID})
}

// handlePairRequest is POST /api/pair/request: a browser asking for a
// fresh code, which is printed in the server's terminal and never
// returned — a code handed to whoever asked would authenticate them.
//
// Asking is not logging in, so it cannot take anything from a code that
// is already out: while one is live the answer is only that it is (the
// terminal shows it), with its guesses and its name left as they were. A
// new code comes once that one is used, expired or burned, and not at all
// while too many wrong guesses have pairing locked.
func (s *Server) handlePairRequest(w http.ResponseWriter, r *http.Request) {
	if s.opt.OpenAccess {
		writeError(w, http.StatusConflict, "this board is served without pairing")
		return
	}
	if _, authed := s.who(r); authed {
		writeError(w, http.StatusConflict, "this browser is already paired")
		return
	}
	if !s.mints.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "a code was just printed; check the terminal running `gummi web`")
		return
	}
	code, expires, err := s.opt.Pairing.Request()
	var locked *LockedError
	switch {
	case errors.Is(err, ErrCodeLive):
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":            true,
			"live":          true,
			"expiresInSecs": int(expires.Sub(s.now()).Seconds()),
		})
		return
	case errors.As(err, &locked):
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.opt.Log("web: pairing code %s (asked for by %s, good for %s)", code, clientIP(r), codeTTL)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"expiresInSecs": int(expires.Sub(s.now()).Seconds()),
	})
}

// handleUnpair is POST /api/unpair: this browser forgets itself, and the
// board forgets it back — its notifications stop and its open event
// streams (other tabs) close, so it leaves the viewer list at once.
func (s *Server) handleUnpair(w http.ResponseWriter, r *http.Request) {
	if s.opt.OpenAccess {
		writeError(w, http.StatusConflict, "this board is served without pairing")
		return
	}
	who, _ := WhoFrom(r.Context())
	if _, err := s.opt.Devices.Forget(who.DeviceID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.opt.Push != nil {
		if err := s.opt.Push.Store.Remove(who.DeviceID); err != nil {
			s.opt.Log("web: dropping %s's notifications: %v", who.DeviceID, err)
		}
	}
	s.hub.dropDevice(who.DeviceID)
	s.setDeviceCookie(w, r, "")
	s.opt.Log("web: unpaired %s on %s (%s)", who.Person, who.Device, who.DeviceID)
	writeJSON(w, http.StatusOK, webapi.OK{OK: true})
}

// handleAdminPair backs `gummi web pair`: a second terminal on this
// machine asks the running server for a code. It is loopback-only and
// carries a token written 0600 beside the devices file, so reaching it
// means already being on the machine with the operator's own permissions.
func (s *Server) handleAdminPair(w http.ResponseWriter, r *http.Request) {
	if s.opt.AdminToken == "" {
		http.NotFound(w, r)
		return
	}
	if !isLoopback(clientIP(r)) {
		writeError(w, http.StatusForbidden, "the admin route only answers on loopback")
		return
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.opt.AdminToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "bad admin token")
		return
	}
	var body webapi.AdminPairRequest
	if r.ContentLength != 0 {
		if err := readJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "expected an empty body or {\"name\": …}")
			return
		}
	}
	person := ""
	if strings.TrimSpace(body.Name) != "" {
		var err error
		if person, err = personName(body.Name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	code, expires, err := s.opt.Pairing.MintFor(person)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if person != "" {
		s.opt.Log("web: a pairing code was printed for %s by `gummi web pair`", person)
	} else {
		s.opt.Log("web: a pairing code was printed by `gummi web pair`")
	}
	writeJSON(w, http.StatusOK, webapi.AdminPairResponse{
		Code:          code,
		ExpiresInSecs: int(expires.Sub(s.now()).Seconds()),
	})
}

// NewAdminToken mints the token that authenticates `gummi web pair`.
func NewAdminToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating the admin token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
