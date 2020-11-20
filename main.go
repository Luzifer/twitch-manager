package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/Luzifer/rconfig/v2"
)

var (
	cfg = struct {
		AssetCheckInterval    time.Duration `flag:"asset-check-interval" default:"1m" description:"How often to check asset files for updates"`
		AssetDir              string        `flag:"asset-dir" default:"." description:"Directory containing assets"`
		BaseURL               string        `flag:"base-url" default:"" description:"Base URL of this service" validate:"nonzero"`
		ForceSyncInterval     time.Duration `flag:"force-sync-interval" default:"1m" description:"How often to force a sync without updates"`
		Listen                string        `flag:"listen" default:":3000" description:"Port/IP to listen on"`
		LogLevel              string        `flag:"log-level" default:"info" description:"Log level (debug, info, warn, error, fatal)"`
		StoreFile             string        `flag:"store-file" default:"store.json.gz" description:"File to store the state to"`
		TwitchClient          string        `flag:"twitch-client" default:"" description:"Client ID to act as" validate:"nonzero"`
		TwitchID              string        `flag:"twitch-id" default:"" description:"ID of the user of the overlay" validate:"nonzero"`
		TwitchToken           string        `flag:"twitch-token" default:"" description:"OAuth token valid for client"`
		UpdateFromAPIInterval time.Duration `flag:"update-from-api-interval" default:"10m" description:"How often to ask the API for real values"`
		VersionAndExit        bool          `flag:"version" default:"false" description:"Prints current version and exits"`
		WebHookTimeout        time.Duration `flag:"webhook-timeout" default:"15m" description:"When to re-register the webhooks"`
	}{}

	assets            = []string{"app.js", "overlay.html"}
	assetVersions     = map[string]string{}
	assetVersionsLock = new(sync.RWMutex)

	store         *storage
	webhookSecret = uuid.Must(uuid.NewV4()).String()

	version = "dev"
)

func init() {
	rconfig.AutoEnv(true)
	if err := rconfig.ParseAndValidate(&cfg); err != nil {
		log.Fatalf("Unable to parse commandline options: %s", err)
	}

	if cfg.VersionAndExit {
		fmt.Printf("twitch-manager %s\n", version)
		os.Exit(0)
	}

	if l, err := log.ParseLevel(cfg.LogLevel); err != nil {
		log.WithError(err).Fatal("Unable to parse log level")
	} else {
		log.SetLevel(l)
	}
}

func main() {
	store = newStorage()
	if err := store.Load(cfg.StoreFile); err != nil && !os.IsNotExist(err) {
		log.WithError(err).Fatal("Unable to load store")
	}

	if err := updateAssetHashes(); err != nil {
		log.WithError(err).Fatal("Unable to read asset hashes")
	}

	router := mux.NewRouter()
	registerAPI(router)

	router.HandleFunc(
		fmt.Sprintf("/{file:(?:%s)}", strings.Join(assets, "|")),
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, path.Join(cfg.AssetDir, mux.Vars(r)["file"]))
		},
	)

	go func() {
		if err := http.ListenAndServe(cfg.Listen, router); err != nil {
			log.WithError(err).Fatal("HTTP server ended unexpectedly")
		}
	}()

	if err := registerWebHooks(); err != nil {
		log.WithError(err).Fatal("Unable to register webhooks")
	}

	var (
		timerAssetCheck      = time.NewTicker(cfg.AssetCheckInterval)
		timerForceSync       = time.NewTicker(cfg.ForceSyncInterval)
		timerUpdateFromAPI   = time.NewTicker(cfg.UpdateFromAPIInterval)
		timerWebhookRegister = time.NewTicker(cfg.WebHookTimeout)
	)

	for {
		select {
		case <-timerAssetCheck.C:
			if err := updateAssetHashes(); err != nil {
				log.WithError(err).Error("Unable to update asset hashes")
			}

		case <-timerForceSync.C:
			if err := sendAllSockets(msgTypeStore, store); err != nil {
				log.WithError(err).Error("Unable to send store to all sockets")
			}

		case <-timerUpdateFromAPI.C:
			if err := updateStats(); err != nil {
				log.WithError(err).Error("Unable to update statistics from API")
			}

		case <-timerWebhookRegister.C:
			if err := registerWebHooks(); err != nil {
				log.WithError(err).Fatal("Unable to re-register webhooks")
			}

		}
	}
}

func updateAssetHashes() error {
	assetVersionsLock.Lock()
	defer assetVersionsLock.Unlock()

	for _, asset := range assets {
		hash := sha256.New()
		f, err := os.Open(asset)
		if err != nil {
			return errors.Wrap(err, "open asset file")
		}
		defer f.Close()

		if _, err = io.Copy(hash, f); err != nil {
			return errors.Wrap(err, "read asset file")
		}

		assetVersions[asset] = fmt.Sprintf("%x", hash.Sum(nil))
	}

	return nil
}
