package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/pkg/errors"
)

func getTwitchAppAccessToken(ctx context.Context) (string, error) {
	var rData struct {
		AccessToken  string        `json:"access_token"`
		RefreshToken string        `json:"refresh_token"`
		ExpiresIn    int           `json:"expires_in"`
		Scope        []interface{} `json:"scope"`
		TokenType    string        `json:"token_type"`
	}

	params := make(url.Values)
	params.Set("client_id", cfg.TwitchClient)
	params.Set("client_secret", cfg.TwitchSecret)
	params.Set("grant_type", "client_credentials")

	u, _ := url.Parse("https://id.twitch.tv/oauth2/token")
	u.RawQuery = params.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "fetching response")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", errors.Wrapf(err, "unexpected status %d and cannot read body", resp.StatusCode)
		}
		return "", errors.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	return rData.AccessToken, errors.Wrap(
		json.NewDecoder(resp.Body).Decode(&rData),
		"decoding response",
	)
}
