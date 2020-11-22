package main

import (
	"crypto/sha256"
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
	msgTypeStore string = "store"
)

var (
	socketSubscriptions     = map[string]func(msg interface{}) error{}
	socketSubscriptionsLock = new(sync.RWMutex)
)

func sendAllSockets(msgType string, msg interface{}) error {
	socketSubscriptionsLock.RLock()
	defer socketSubscriptionsLock.RUnlock()

	for _, hdl := range socketSubscriptions {
		if err := hdl(compileSocketMessage(msgType, msg)); err != nil {
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

func compileSocketMessage(msgType string, msg interface{}) map[string]interface{} {
	assetVersionsLock.RLock()
	defer assetVersionsLock.RUnlock()

	versionParts := []string{version}
	for _, asset := range assets {
		versionParts = append(versionParts, assetVersions[asset])
	}

	hash := sha256.New()
	hash.Write([]byte(strings.Join(versionParts, "/")))

	ver := fmt.Sprintf("%x", hash.Sum(nil))

	return map[string]interface{}{
		"payload": msg,
		"type":    msgType,
		"version": ver,
	}
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
	var (
		connLock = new(sync.Mutex)
		id       = uuid.Must(uuid.NewV4()).String()
	)
	subscribeSocket(id, func(msg interface{}) error {
		connLock.Lock()
		defer connLock.Unlock()

		return conn.WriteJSON(msg)
	})
	defer unsubscribeSocket(id)

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
