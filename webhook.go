package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/Luzifer/go_helpers/v2/str"
)

const twitchRequestTimeout = 2 * time.Second

func handleWebHookPush(w http.ResponseWriter, r *http.Request) {
	var (
		vars     = mux.Vars(r)
		hookType = vars["type"]

		logger = log.WithField("type", hookType)
	)

	// When asked for a confirmation, just confirm it
	if challengeToken := r.URL.Query().Get("hub.challenge"); challengeToken != "" {
		logger.Debug("Confirming webhook subscription")
		w.Write([]byte(challengeToken))
		return
	}

	// We're getting a reason for a denied subscription
	if reason := r.URL.Query().Get("hub.reason"); reason != "" {
		logger.WithField("reason", reason).Error("Webhook subscription was denied")
		return
	}

	var (
		body      = new(bytes.Buffer)
		signature = r.Header.Get("X-Hub-Signature")
	)

	if _, err := io.Copy(body, r.Body); err != nil {
		logger.WithError(err).Error("Unable to read hook body")
		return
	}

	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(body.Bytes())
	if cSig := fmt.Sprintf("sha256=%x", mac.Sum(nil)); cSig != signature {
		log.Errorf("Got message signature %s, expected %s", signature, cSig)
		http.Error(w, "Signature verification failed", http.StatusUnauthorized)
		return
	}

	switch hookType {
	case "follow":
		var payload struct {
			Data []struct {
				FromName   string    `json:"from_name"`
				FollowedAt time.Time `json:"followed_at"`
			} `json:"data"`
		}

		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			logger.WithError(err).Error("Unable to decode payload")
			return
		}

		sort.Slice(payload.Data, func(i, j int) bool { return payload.Data[i].FollowedAt.Before(payload.Data[j].FollowedAt) })
		for _, f := range payload.Data {
			var isKnown bool

			store.WithModRLock(func() error {
				isKnown = str.StringInSlice(f.FromName, store.Followers.Seen)
				return nil
			})

			if isKnown {
				logger.WithField("name", f.FromName).Debug("New follower already known, skipping")
				continue
			}

			logger.WithField("name", f.FromName).Info("New follower announced")
			store.WithModLock(func() error {
				store.Followers.Last = &f.FromName
				store.Followers.Count++
				store.Followers.Seen = append([]string{f.FromName}, store.Followers.Seen...)

				return nil
			})
		}

	default:
		log.WithField("type", hookType).Warn("Received unexpected webhook request")
		return
	}

	if err := store.Save(cfg.StoreFile); err != nil {
		logger.WithError(err).Error("Unable to update persistent store")
	}

	if err := store.WithModRLock(func() error { return subscriptions.SendAllSockets(msgTypeStore, store) }); err != nil {
		logger.WithError(err).Error("Unable to send update to all sockets")
	}
}

func registerWebHooks() error {
	hookURL := func(hookType string) string {
		return strings.Join([]string{
			strings.TrimRight(cfg.BaseURL, "/"),
			"api", "webhook",
			hookType,
		}, "/")
	}

	for uri, topic := range map[string]string{
		hookURL("follow"): fmt.Sprintf("https://api.twitch.tv/helix/users/follows?first=1&to_id=%s", cfg.TwitchID),
	} {
		ctx, cancel := context.WithTimeout(context.Background(), twitchRequestTimeout)
		defer cancel()

		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(map[string]interface{}{
			"hub.callback":      uri,
			"hub.mode":          "subscribe",
			"hub.topic":         topic,
			"hub.lease_seconds": int64((cfg.WebHookTimeout + twitchRequestTimeout) / time.Second),
			"hub.secret":        webhookSecret,
		}); err != nil {
			return errors.Wrap(err, "assemble subscribe payload")
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.twitch.tv/helix/webhooks/hub", buf)
		if err != nil {
			return errors.Wrap(err, "assemble subscribe request")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Client-Id", cfg.TwitchClient)
		req.Header.Set("Authorization", "Bearer "+cfg.TwitchToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return errors.Wrap(err, "requesting subscribe")
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return errors.Wrapf(err, "unexpected status %d, unable to read body", resp.StatusCode)
			}
			return errors.Errorf("unexpected status %d: %s", resp.StatusCode, body)
		}
	}

	return nil
}
