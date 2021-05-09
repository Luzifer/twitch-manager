package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func updateStats() error {
	log.Debug("Updating statistics from API")
	for _, fn := range []func() error{
		updateFollowers,
		updateSubscriberCount,
		func() error { return subscriptions.SendAllSockets(msgTypeStore, store, false, false) },
	} {
		if err := fn(); err != nil {
			return errors.Wrap(err, "update statistics module")
		}
	}

	return nil
}

func updateFollowers() error {
	log.Debug("Updating followers from API")
	ctx, cancel := context.WithTimeout(context.Background(), twitchRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://api.twitch.tv/helix/users/follows?to_id=%s", cfg.TwitchID), nil)
	if err != nil {
		return errors.Wrap(err, "assemble follower count request")
	}
	req.Header.Set("Client-Id", cfg.TwitchClient)
	req.Header.Set("Authorization", "Bearer "+cfg.TwitchToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "requesting subscribe")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return errors.Wrapf(err, "unexpected status %d, unable to read body", resp.StatusCode)
		}
		return errors.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	payload := struct {
		Total int64 `json:"total"`
		Data  []struct {
			FromName   string    `json:"from_name"`
			FollowedAt time.Time `json:"followed_at"`
		} `json:"data"`
		// Contains more but I don't care.
	}{}

	if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return errors.Wrap(err, "decode json response")
	}

	var seen []string
	for _, f := range payload.Data {
		seen = append(seen, f.FromName)
	}

	store.WithModLock(func() error {
		store.Followers.Count = payload.Total
		store.Followers.Seen = seen

		return nil
	})

	return errors.Wrap(store.Save(cfg.StoreFile), "save store")
}

func updateSubscriberCount() error {
	log.Debug("Updating subscriber count from API")

	var (
		params   = url.Values{"broadcaster_id": []string{cfg.TwitchID}}
		subCount int64
	)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), twitchRequestTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://api.twitch.tv/helix/subscriptions?%s", params.Encode()), nil)
		if err != nil {
			return errors.Wrap(err, "assemble subscriber request")
		}
		req.Header.Set("Client-Id", cfg.TwitchClient)
		req.Header.Set("Authorization", "Bearer "+cfg.TwitchToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return errors.Wrap(err, "requesting subscribe")
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return errors.Wrapf(err, "unexpected status %d, unable to read body", resp.StatusCode)
			}
			return errors.Errorf("unexpected status %d: %s", resp.StatusCode, body)
		}

		payload := struct {
			Pagination struct {
				Cursor string `json:"cursor"`
			} `json:"pagination"`
			Data []struct {
				BroadcasterID string `json:"broadcaster_id"`
				Tier          string `json:"tier"`
				UserID        string `json:"user_id"`
			} `json:"data"`
			// Contains more but I don't care.
		}{}

		if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return errors.Wrap(err, "decode json response")
		}

		if len(payload.Data) == 0 {
			break
		}

		for _, sub := range payload.Data {
			if sub.UserID == sub.BroadcasterID {
				// Don't count self
				continue
			}

			subCount++
		}

		params.Set("after", payload.Pagination.Cursor)
	}

	store.WithModLock(func() error {
		store.Subs.Count = subCount

		return nil
	})

	return errors.Wrap(store.Save(cfg.StoreFile), "save store")
}
