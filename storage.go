package main

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/pkg/errors"
)

const storeMaxRecent = 50

type subscriber struct {
	Name   string `json:"name"`
	Months int64  `json:"months"`
}

type storedEvent struct {
	Time    time.Time
	Type    string
	Message map[string]interface{}
}

type storage struct {
	BitDonations struct {
		LastDonator  *string          `json:"last_donator"`
		LastAmount   int64            `json:"last_amount"`
		TotalAmounts map[string]int64 `json:"total_amounts"`
	} `json:"bit_donations"`
	Donations struct {
		LastDonator *string `json:"last_donator"`
		LastAmount  float64 `json:"last_amount"`
	} `json:"donations"`
	Followers struct {
		Last  *string  `json:"last"`
		Seen  []string `json:"seen"`
		Count int64    `json:"count"`
	} `json:"followers"`
	Subs struct {
		Last         *string      `json:"last"`
		LastDuration int64        `json:"last_duration"`
		Count        int64        `json:"count"`
		Recent       []subscriber `json:"recent"`
	} `json:"subs"`

	Events []storedEvent

	modLock  sync.RWMutex
	saveLock sync.Mutex
}

func newStorage() *storage { return &storage{} }

func (s *storage) Load(from string) error {
	f, err := os.Open(from)
	if err != nil {
		if os.IsNotExist(err) {
			return err
		}
		return errors.Wrap(err, "opening storage file")
	}
	defer f.Close()

	gf, err := gzip.NewReader(f)
	if err != nil {
		return errors.Wrap(err, "create gzip reader")
	}
	defer gf.Close()

	return errors.Wrap(
		json.NewDecoder(gf).Decode(s),
		"decode json",
	)
}

func (s *storage) Save(to string) error {
	s.modLock.RLock()
	defer s.modLock.RUnlock()

	s.saveLock.Lock()
	defer s.saveLock.Unlock()

	if len(s.Followers.Seen) > storeMaxRecent {
		s.Followers.Seen = s.Followers.Seen[:storeMaxRecent]
	}

	if len(s.Subs.Recent) > storeMaxRecent {
		s.Subs.Recent = s.Subs.Recent[:storeMaxRecent]
	}

	sort.Slice(s.Events, func(j, i int) bool { return s.Events[i].Time.Before(s.Events[j].Time) })
	if len(s.Events) > storeMaxRecent {
		s.Events = s.Events[:storeMaxRecent]
	}

	f, err := os.Create(to)
	if err != nil {
		return errors.Wrap(err, "create file")
	}
	defer f.Close()

	gf := gzip.NewWriter(f)
	defer gf.Close()

	return errors.Wrap(
		json.NewEncoder(gf).Encode(s),
		"encode json",
	)
}

func (s *storage) WithModLock(fn func() error) error {
	s.modLock.Lock()
	defer s.modLock.Unlock()

	return fn()
}

func (s *storage) WithModRLock(fn func() error) error {
	s.modLock.RLock()
	defer s.modLock.RUnlock()

	return fn()
}
