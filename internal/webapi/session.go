package webapi

import "time"

// Session is GET /api/session: who the page is talking to and as whom.
// It answers without a cookie, so the page can decide between the pairing
// form and the board — and to a browser that is not paired it says only
// Authed, OpenAccess and PairingLive: nothing about the board before a
// device has earned it. POST /api/unpair (no body) is the other end of a
// pairing: the browser forgets itself and the board forgets it back.
type Session struct {
	Authed bool `json:"authed"`
	// OpenAccess is a board served with --no-pairing (loopback only).
	OpenAccess bool `json:"openAccess,omitempty"`
	// Person and Device name the paired browser; empty when not authed.
	Person string `json:"person,omitempty"`
	Device string `json:"device,omitempty"`
	// DeviceID is the paired device's id, as `gummi web devices` lists it.
	DeviceID string `json:"deviceId,omitempty"`
	// Version is the gummi binary's version stamp (paired callers only).
	Version string `json:"version"`
	// Repo names the workspace, its directory's base name (paired
	// callers only).
	Repo string `json:"repo"`
	// Host is the machine the board runs on (paired callers only).
	Host string `json:"host"`
	// PairingLive reports a code is currently redeemable, so the form can
	// say "enter the code" rather than "run gummi web pair".
	PairingLive bool `json:"pairingLive,omitempty"`
	// PairingFor is the person the live code was minted for. It is never
	// sent: to a browser that is not paired it would name a person, and a
	// paired one has no code to redeem. The server takes the code's own
	// name when the form sends none.
	PairingFor string `json:"pairingFor,omitempty"`
	// Approval is where a paired browser that does not have the board
	// stands (Authed is false for it): ApprovalPending while it waits for
	// a device already at the board to let it in — Person, Device and
	// DeviceID then name it, and ExpiresInSecs is how long the request
	// has left — or ApprovalRejected / ApprovalExpired once that wait
	// ended without it, so the pairing form can say why it is back.
	Approval      string `json:"approval,omitempty"`
	ExpiresInSecs int    `json:"expiresInSecs,omitempty"`
}

// The approval states a Session (and a 403's Error) can name (DESIGN
// §20.3). A device paired while another already has the board, with a
// code other than the one printed when the server started, waits to be
// let in from a paired page.
const (
	ApprovalPending  = "pending"
	ApprovalRejected = "rejected"
	ApprovalExpired  = "expired"
)

// PairRequest is POST /api/pair's body: the six-digit code printed in the
// server's terminal, and the name of the person pairing. Devices paired
// under one name are one person (§20.3). Name may be empty when the code
// was minted for a named person (`gummi web pair --name`); that name wins.
type PairRequest struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// PairResponse is POST /api/pair's success body. The device token travels
// only in the Set-Cookie header, never in JSON. Pending says the device
// is paired but waits to be let in from a device already at the board,
// for ExpiresInSecs at most.
type PairResponse struct {
	Person        string `json:"person"`
	Device        string `json:"device"`
	DeviceID      string `json:"deviceId"`
	Pending       bool   `json:"pending,omitempty"`
	ExpiresInSecs int    `json:"expiresInSecs,omitempty"`
}

// PendingDevice is one device waiting to be let in, as a page already at
// the board shows it to the person who decides: who it says it is, what
// browser, from where, with which code, and when.
type PendingDevice struct {
	ID     string `json:"id"`
	Person string `json:"person"`
	// Device is the label read off UserAgent ("iPhone · Safari").
	Device    string `json:"device"`
	UserAgent string `json:"userAgent,omitempty"`
	// Source is the address the pairing came from.
	Source string `json:"source,omitempty"`
	// Code is who minted the code it used ("cli", "browser"), and Via
	// says the same as a phrase ("via the local CLI (`gummi web pair`)").
	Code string `json:"code"`
	Via  string `json:"via"`
	// Origin is the host and port it paired on.
	Origin        string    `json:"origin,omitempty"`
	RequestedAt   time.Time `json:"requestedAt"`
	ExpiresInSecs int       `json:"expiresInSecs"`
}

// PendingDevices is GET /api/devices/pending: every device waiting to be
// let in, oldest first. POST /api/devices/{id}/approve and
// POST /api/devices/{id}/reject (no body) answer one; either is refused
// 409 once the request is no longer waiting.
type PendingDevices struct {
	Devices []PendingDevice `json:"devices"`
}

// AdminPairRequest is POST /api/admin/pair's optional body: the person the
// code will pair, when the operator names them (`gummi web pair --name`).
type AdminPairRequest struct {
	Name string `json:"name,omitempty"`
}

// AdminPairResponse is POST /api/admin/pair's body: `gummi web pair` in a
// second terminal asking the running server for a code.
type AdminPairResponse struct {
	Code          string `json:"code"`
	ExpiresInSecs int    `json:"expiresInSecs"`
}

// Viewer is one person at the board right now: a connected event stream.
type Viewer struct {
	Person   string    `json:"person"`
	Device   string    `json:"device"`
	DeviceID string    `json:"deviceId"`
	Since    time.Time `json:"since"`
}
