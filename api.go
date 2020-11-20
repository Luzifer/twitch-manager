package main

import (
	"net/http"
	"sync"
	"time"

	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var (
	socketSubscriptions     = map[string]func(msg interface{}) error{}
	socketSubscriptionsLock = new(sync.RWMutex)
)

func sendAllSockets(msg interface{}) error {
	socketSubscriptionsLock.RLock()
	defer socketSubscriptionsLock.RUnlock()

	for _, hdl := range socketSubscriptions {
		if err := hdl(msg); err != nil {
			return errors.Wrap(err, "submit message")
		}
	}

	return nil
}

func subscribeSocket(id string, hdl func(interface{}) error) {
	socketSubscriptionsLock.Lock()
	defer socketSubscriptionsLock.Unlock()

	socketSubscriptions[id] = hdl
}

func unsubscribeSocket(id string) {
	socketSubscriptionsLock.Lock()
	defer socketSubscriptionsLock.Unlock()

	delete(socketSubscriptions, id)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func registerAPI(r *mux.Router) {
	r.HandleFunc("/api/subscribe", handleUpdateSocket)
	r.HandleFunc("/api/webhook/{type}", handleWebHookPush)
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
	id := uuid.Must(uuid.NewV4()).String()
	subscribeSocket(id, conn.WriteJSON)
	defer unsubscribeSocket(id)

	keepAlive := time.NewTicker(5 * time.Second)
	defer keepAlive.Stop()
	go func() {
		for range keepAlive.C {
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.WithError(err).Error("Unable to send ping message")
				conn.Close()
			}
		}
	}()

	if err := conn.WriteJSON(store); err != nil {
		log.WithError(err).Error("Unable to send initial state")
		return
	}

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
