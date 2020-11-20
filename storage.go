package main

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"sync"

	"github.com/pkg/errors"
)

const storeMaxFollowers = 25

type storage struct {
	Donations struct {
		LastDonator *string `json:"last_donator"`
		LastAmount  float64 `json:"last_amount"`
		TotalAmount float64 `json:"total_amount"`
	} `json:"donations"`
	Followers struct {
		Last  *string  `json:"last"`
		Seen  []string `json:"seen"`
		Count int64    `json:"count"`
	} `json:"followers"`
	Subs struct {
		Last         *string `json:"last"`
		LastDuration int64   `json:"last_duration"`
		Count        int64   `json:"count"`
	} `json:"subs"`

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
	s.saveLock.Lock()
	defer s.saveLock.Unlock()

	if len(s.Followers.Seen) > storeMaxFollowers {
		s.Followers.Seen = s.Followers.Seen[:storeMaxFollowers]
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
