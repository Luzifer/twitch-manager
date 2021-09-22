package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

const twitchRequestTimeout = 2 * time.Second

func handleWebHookPush(w http.ResponseWriter, r *http.Request) {
	var (
		vars     = mux.Vars(r)
		hookType = vars["type"]

		logger = log.WithField("type", hookType)
	)

	var (
		body      = new(bytes.Buffer)
		signature = r.Header.Get("X-Hub-Signature")
	)

	if _, err := io.Copy(body, r.Body); err != nil {
		logger.WithError(err).Error("Unable to read hook body")
		return
	}

	mac := hmac.New(sha256.New, []byte(cfg.WebHookSecret))
	mac.Write(body.Bytes())
	if cSig := fmt.Sprintf("sha256=%x", mac.Sum(nil)); cSig != signature {
		log.Errorf("Got message signature %s, expected %s", signature, cSig)
		http.Error(w, "Signature verification failed", http.StatusUnauthorized)
		return
	}

	switch hookType {
	case "donation":
		var payload struct {
			Name    string  `json:"name"`
			Amount  float64 `json:"amount"`
			Message string  `json:"message"`
		}

		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			logger.WithError(err).Error("Unable to decode payload")
			return
		}

		fields := map[string]interface{}{
			"name":    payload.Name,
			"amount":  payload.Amount,
			"message": payload.Message,
		}

		if err := subscriptions.SendAllSockets(msgTypeDonation, fields, false, true); err != nil {
			log.WithError(err).Error("Unable to send update to all sockets")
		}

		store.WithModLock(func() error {
			store.Donations.LastAmount = payload.Amount
			store.Donations.LastDonator = &payload.Name

			return nil
		})

	default:
		log.WithField("type", hookType).Warn("Received unexpected webhook request")
		return
	}

	if err := store.Save(cfg.StoreFile); err != nil {
		logger.WithError(err).Error("Unable to update persistent store")
	}

	if err := store.WithModRLock(func() error { return subscriptions.SendAllSockets(msgTypeStore, store, false, false) }); err != nil {
		logger.WithError(err).Error("Unable to send update to all sockets")
	}
}
