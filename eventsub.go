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
	"strings"
	"time"

	"github.com/Luzifer/go_helpers/v2/str"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	eventSubHeaderMessageID           = "Twitch-Eventsub-Message-Id"
	eventSubHeaderMessageRetry        = "Twitch-Eventsub-Message-Retry"
	eventSubHeaderMessageType         = "Twitch-Eventsub-Message-Type"
	eventSubHeaderMessageSignature    = "Twitch-Eventsub-Message-Signature"
	eventSubHeaderMessageTimestamp    = "Twitch-Eventsub-Message-Timestamp"
	eventSubHeaderSubscriptionType    = "Twitch-Eventsub-Subscription-Type"
	eventSubHeaderSubscriptionVersion = "Twitch-Eventsub-Subscription-Version"

	eventSubMessageTypeNotification = "notification"
	eventSubMessageTypeVerification = "webhook_callback_verification"
	eventSubMessageTypeRevokation   = "revocation"

	eventSubStatusAuthorizationRevoked = "authorization_revoked"
	eventSubStatusEnabled              = "enabled"
	eventSubStatusFailuresExceeded     = "notification_failures_exceeded"
	eventSubStatusUserRemoved          = "user_removed"
	eventSubStatusVerificationFailed   = "webhook_callback_verification_failed"
	eventSubStatusVerificationPending  = "webhook_callback_verification_pending"
)

type (
	eventSubCondition struct {
		BroadcasterUserID string `json:"broadcaster_user_id,omitempty"`
	}
	eventSubEventFollow struct {
		UserID               string    `json:"user_id"`
		UserLogin            string    `json:"user_login"`
		UserName             string    `json:"user_name"`
		BroadcasterUserID    string    `json:"broadcaster_user_id"`
		BroadcasterUserLogin string    `json:"broadcaster_user_login"`
		BroadcasterUserName  string    `json:"broadcaster_user_name"`
		FollowedAt           time.Time `json:"followed_at"`
	}
	eventSubPostMessage struct {
		Challenge    string               `json:"challenge"`
		Subscription eventSubSubscription `json:"subscription"`
		Event        json.RawMessage      `json:"event"`
	}
	eventSubSubscription struct {
		ID        string            `json:"id,omitempty"`     // READONLY
		Status    string            `json:"status,omitempty"` // READONLY
		Type      string            `json:"type"`
		Version   string            `json:"version"`
		Cost      int64             `json:"cost,omitempty"` // READONLY
		Condition eventSubCondition `json:"condition"`
		Transport eventSubTransport `json:"transport"`
		CreatedAt time.Time         `json:"created_at,omitempty"` // READONLY
	}
	eventSubTransport struct {
		Method   string `json:"method"`
		Callback string `json:"callback"`
		Secret   string `json:"secret"`
	}
)

func handleEventsubPush(w http.ResponseWriter, r *http.Request) {
	var (
		body      = new(bytes.Buffer)
		message   eventSubPostMessage
		signature = r.Header.Get(eventSubHeaderMessageSignature)
	)

	// Copy body for duplicate processing
	if _, err := io.Copy(body, r.Body); err != nil {
		log.WithError(err).Error("Unable to read hook body")
		return
	}

	// Verify signature
	mac := hmac.New(sha256.New, []byte(cfg.WebHookSecret))
	fmt.Fprintf(mac, "%s%s%s", r.Header.Get(eventSubHeaderMessageID), r.Header.Get(eventSubHeaderMessageTimestamp), body.Bytes())
	if cSig := fmt.Sprintf("sha256=%x", mac.Sum(nil)); cSig != signature {
		log.Errorf("Got message signature %s, expected %s", signature, cSig)
		http.Error(w, "Signature verification failed", http.StatusUnauthorized)
		return
	}

	// Read message
	if err := json.NewDecoder(body).Decode(&message); err != nil {
		log.WithError(err).Errorf("Unable to decode eventsub message")
		http.Error(w, errors.Wrap(err, "parsing message").Error(), http.StatusBadRequest)
		return
	}

	logger := log.WithField("type", message.Subscription.Type)

	// If we got a verification request, respond with the challenge
	switch r.Header.Get(eventSubHeaderMessageType) {
	case eventSubMessageTypeRevokation:
		w.WriteHeader(http.StatusNoContent)
		return

	case eventSubMessageTypeVerification:
		log.WithFields(log.Fields{
			"type": message.Subscription.Type,
		}).Debug("Confirming eventsub subscription")
		w.Write([]byte(message.Challenge))
		return
	}

	switch message.Subscription.Type {
	case "channel.follow":
		var evt eventSubEventFollow
		if err := json.Unmarshal(message.Event, &evt); err != nil {
			log.WithError(err).Errorf("Unable to decode eventsub event payload")
			http.Error(w, errors.Wrap(err, "parsing message").Error(), http.StatusBadRequest)
			return
		}

		logger = logger.WithField("name", evt.UserLogin)

		var isKnown bool
		store.WithModRLock(func() error {
			isKnown = str.StringInSlice(evt.UserLogin, store.Followers.Seen)
			return nil
		})

		if isKnown {
			logger.WithField("name", evt.UserLogin).Debug("New follower already known, skipping")
			return
		}

		fields := map[string]interface{}{
			"from":        evt.UserLogin,
			"followed_at": evt.FollowedAt,
		}

		if err := subscriptions.SendAllSockets(msgTypeFollow, fields, false, true); err != nil {
			log.WithError(err).Error("Unable to send update to all sockets")
		}

		logger.Info("New follower announced")
		store.WithModLock(func() error {
			store.Followers.Last = &evt.UserLogin
			store.Followers.Count++
			store.Followers.Seen = append([]string{evt.UserLogin}, store.Followers.Seen...)

			return nil
		})

	default:
		logger.Warn("Received unexpected webhook request")
		return

	}

	if err := store.Save(cfg.StoreFile); err != nil {
		logger.WithError(err).Error("Unable to update persistent store")
	}

	if err := store.WithModRLock(func() error { return subscriptions.SendAllSockets(msgTypeStore, store, false, false) }); err != nil {
		logger.WithError(err).Error("Unable to send update to all sockets")
	}
}

func registerEventSubHooks() error {
	hookURL := strings.Join([]string{
		strings.TrimRight(cfg.BaseURL, "/"),
		"api", "eventsub",
	}, "/")

	ctx, cancel := context.WithTimeout(context.Background(), twitchRequestTimeout)
	defer cancel()
	accessToken, err := getTwitchAppAccessToken(ctx)
	if err != nil {
		return errors.Wrap(err, "getting app-access-token")
	}

	// List existing subscriptions
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twitch.tv/helix/eventsub/subscriptions", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Id", cfg.TwitchClient)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "requesting subscribscriptions")
	}
	defer resp.Body.Close()

	var subscriptionList struct {
		Data []eventSubSubscription
	}

	if err = json.NewDecoder(resp.Body).Decode(&subscriptionList); err != nil {
		return errors.Wrap(err, "decoding subscription list")
	}

	// Register subscriptions
	for _, event := range []string{
		"channel.follow",
	} {
		var (
			logger             = log.WithField("event", event)
			subscriptionExists bool
		)

		for _, sub := range subscriptionList.Data {
			if str.StringInSlice(sub.Status, []string{eventSubStatusEnabled, eventSubStatusVerificationPending}) && sub.Transport.Callback == hookURL && sub.Type == event {
				logger = logger.WithFields(log.Fields{
					"id":     sub.ID,
					"status": sub.Status,
				})
				subscriptionExists = true
			}
		}

		if subscriptionExists {
			logger.WithField("event", event).Debug("Not registering hook, already active")
			continue
		}

		payload := eventSubSubscription{
			Type:    event,
			Version: "1",
			Condition: eventSubCondition{
				BroadcasterUserID: cfg.TwitchID,
			},
			Transport: eventSubTransport{
				Method:   "webhook",
				Callback: hookURL,
				Secret:   cfg.WebHookSecret,
			},
		}

		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(payload); err != nil {
			return errors.Wrap(err, "assemble subscribe payload")
		}

		ctx, cancel := context.WithTimeout(context.Background(), twitchRequestTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.twitch.tv/helix/eventsub/subscriptions", buf)
		if err != nil {
			return errors.Wrap(err, "creating subscribe request")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Client-Id", cfg.TwitchClient)
		req.Header.Set("Authorization", "Bearer "+accessToken)

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

		logger.Debug("Registered eventsub subscription")
	}

	return nil
}
