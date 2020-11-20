package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func updateStats() error {
	log.Debug("Updating statistics from API")
	for _, fn := range []func() error{
		updateFollowers,
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

	store.Followers.Count = payload.Total
	store.Followers.Seen = seen

	if err = store.Save(cfg.StoreFile); err != nil {
		return errors.Wrap(err, "save store")
	}

	return errors.Wrap(
		sendAllSockets(store),
		"update all sockets",
	)
}
