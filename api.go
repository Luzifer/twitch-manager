package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

const (
	msgTypeHost  string = "host"
	msgTypeRaid  string = "raid"
	msgTypeStore string = "store"
)

var subscriptions = newSubscriptionStore()

type socketMessage struct {
	Payload interface{} `json:"payload"`
	Type    string      `json:"type"`
	Version string      `json:"version"`
}

type subcriptionStore struct {
	socketSubscriptions     map[string]func(socketMessage) error
	socketSubscriptionsLock *sync.RWMutex
}

func newSubscriptionStore() *subcriptionStore {
	return &subcriptionStore{
		socketSubscriptions:     map[string]func(socketMessage) error{},
		socketSubscriptionsLock: new(sync.RWMutex),
	}
}

func (s subcriptionStore) SendAllSockets(msgType string, msg interface{}) error {
	s.socketSubscriptionsLock.RLock()
	defer s.socketSubscriptionsLock.RUnlock()

	for _, hdl := range s.socketSubscriptions {
		if err := hdl(compileSocketMessage(msgType, msg)); err != nil {
			return errors.Wrap(err, "submit message")
		}
	}

	return nil
}

func (s *subcriptionStore) SubscribeSocket(id string, hdl func(socketMessage) error) {
	s.socketSubscriptionsLock.Lock()
	defer s.socketSubscriptionsLock.Unlock()

	s.socketSubscriptions[id] = hdl
}

func (s *subcriptionStore) UnsubscribeSocket(id string) {
	s.socketSubscriptionsLock.Lock()
	defer s.socketSubscriptionsLock.Unlock()

	delete(s.socketSubscriptions, id)
}

func compileSocketMessage(msgType string, msg interface{}) socketMessage {
	versionParts := []string{version}
	for _, asset := range assetVersions.Keys() {
		versionParts = append(versionParts, assetVersions.Get(asset))
	}

	hash := sha256.New()
	hash.Write([]byte(strings.Join(versionParts, "/")))

	ver := fmt.Sprintf("%x", hash.Sum(nil))

	return socketMessage{
		Payload: msg,
		Type:    msgType,
		Version: ver,
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func registerAPI(r *mux.Router) {
	r.HandleFunc("/api/custom-alert", handleCustomAlert).Methods(http.MethodPost)
	r.HandleFunc("/api/follows/clear-last", handleSetLastFollower).Methods(http.MethodPut)
	r.HandleFunc("/api/follows/set-last/{name}", handleSetLastFollower).Methods(http.MethodPut)
	r.HandleFunc("/api/subscribe", handleUpdateSocket).Methods(http.MethodGet)
	r.HandleFunc("/api/webhook/{type}", handleWebHookPush)
}

func handleCustomAlert(w http.ResponseWriter, r *http.Request) {
	var alert struct {
		Sound   *string `json:"sound"`
		Text    string  `json:"text"`
		Title   string  `json:"title"`
		Variant *string `json:"variant"`
	}

	if err := json.NewDecoder(r.Body).Decode(&alert); err != nil {
		http.Error(w, errors.Wrap(err, "parse request body").Error(), http.StatusBadRequest)
		return
	}

	if alert.Title == "" || alert.Text == "" {
		http.Error(w, "empty title or text", http.StatusBadRequest)
		return
	}

	if err := subscriptions.SendAllSockets("alert", alert); err != nil {
		http.Error(w, errors.Wrap(err, "send to sockets").Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func handleSetLastFollower(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	if name == "" {
		store.Followers.Last = nil
	} else {
		store.Followers.Last = &name
	}

	if err := store.Save(cfg.StoreFile); err != nil {
		log.WithError(err).Error("Unable to update persistent store")
	}

	if err := subscriptions.SendAllSockets(msgTypeStore, store); err != nil {
		log.WithError(err).Error("Unable to send update to all sockets")
	}

	w.WriteHeader(http.StatusAccepted)
}

func handleUpdateSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade connection to socket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).Error("Unable to upgrade socket")
		return
	}
	defer conn.Close()

	// Register listener
	var (
		connLock = new(sync.Mutex)
		id       = uuid.Must(uuid.NewV4()).String()
	)
	subscriptions.SubscribeSocket(id, func(msg socketMessage) error {
		connLock.Lock()
		defer connLock.Unlock()

		return conn.WriteJSON(msg)
	})
	defer subscriptions.UnsubscribeSocket(id)

	keepAlive := time.NewTicker(5 * time.Second)
	defer keepAlive.Stop()
	go func() {
		for range keepAlive.C {
			connLock.Lock()

			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.WithError(err).Error("Unable to send ping message")
				connLock.Unlock()
				conn.Close()
				return
			}

			connLock.Unlock()
		}
	}()

	connLock.Lock()
	if err := conn.WriteJSON(compileSocketMessage(msgTypeStore, store)); err != nil {
		log.WithError(err).Error("Unable to send initial state")
		return
	}
	connLock.Unlock()

	// Handle socket
	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			log.WithError(err).Error("Unable to read from socket")
			return
		}

		switch messageType {
		case websocket.TextMessage:
			// This is fine and expected

		case websocket.BinaryMessage:
			// Wat?
			log.Warn("Got binary message from socket, disconnecting...")
			return

		case websocket.CloseMessage:
			// They want to go? Fine, have it that way!
			return

		default:
			log.Debug("Got unhandled message from socket: %d", messageType)
			continue
		}

		// FIXME: Do we need this?
		_ = p
	}
}
