package web

import (
	"errors"
	"io"
	"net/http"

	"github.com/morphis/gummi/internal/web/push"
	"github.com/morphis/gummi/internal/webapi"
)

func (s *Server) pushRoutes() {
	s.api("GET /api/push/key", s.handlePushKey)
	s.api("POST /api/push/subscribe", s.handlePushSubscribe)
	s.api("DELETE /api/push/subscribe", s.handlePushUnsubscribe)
}

// pushOff answers a push route on a server with no push configured.
func (s *Server) pushOff(w http.ResponseWriter) bool {
	if s.opt.Push == nil {
		writeError(w, http.StatusNotFound, "notifications are not set up on this server")
		return true
	}
	return false
}

// handlePushKey is GET /api/push/key: the key PushManager.subscribe
// takes as its applicationServerKey.
func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if s.pushOff(w) {
		return
	}
	writeJSON(w, http.StatusOK, webapi.PushKey{Key: s.opt.Push.Key()})
}

// handlePushSubscribe is POST /api/push/subscribe: this device's browser
// subscription, kept against the device (one per device; subscribing
// again replaces it) and the person it is paired as.
func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.pushOff(w) {
		return
	}
	var body webapi.PushSubscription
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "expected a PushSubscription as JSON")
		return
	}
	who, _ := WhoFrom(r.Context())
	sub := push.Subscription{
		Endpoint: body.Endpoint,
		Keys:     push.Keys{P256dh: body.Keys.P256dh, Auth: body.Keys.Auth},
		Device:   who.DeviceID,
		Person:   who.Person,
	}
	if err := sub.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.opt.Push.CheckEndpoint(r.Context(), sub.Endpoint); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.opt.Push.Store.Add(sub); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.opt.Log("web: %s on %s subscribed to notifications", who.Person, who.Device)
	writeJSON(w, http.StatusOK, webapi.OK{OK: true})
}

// handlePushUnsubscribe is DELETE /api/push/subscribe: this device stops
// being notified. The body (the browser's subscription, if it sends one)
// is not needed: a device has one subscription, and naming an endpoint
// must not let one device drop another's.
func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if s.pushOff(w) {
		return
	}
	var body webapi.PushSubscription
	if err := readJSON(w, r, &body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "expected an empty body or {\"endpoint\": …}")
		return
	}
	who, _ := WhoFrom(r.Context())
	if err := s.opt.Push.Store.Remove(who.DeviceID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, webapi.OK{OK: true})
}
