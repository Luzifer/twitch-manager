package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/pkg/errors"
)

var assetVersions = newAssetVersionStore()

type assetVersionStore struct {
	store map[string]string
	lock  sync.RWMutex
}

func newAssetVersionStore() *assetVersionStore {
	return &assetVersionStore{
		store: make(map[string]string),
	}
}

func (a *assetVersionStore) Get(key string) string {
	a.lock.RLock()
	defer a.lock.RUnlock()

	return a.store[key]
}

func (a *assetVersionStore) Keys() []string {
	a.lock.RLock()
	defer a.lock.RUnlock()

	var out []string

	for k := range a.store {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}

func (a *assetVersionStore) UpdateAssetHashes(dir string) error {
	a.lock.Lock()
	defer a.lock.Unlock()

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// There was a previous error
			return err
		}

		if info.IsDir() {
			// We can't hash directories
			return nil
		}

		hash := sha256.New()
		f, err := os.Open(path)
		if err != nil {
			return errors.Wrap(err, "open asset file")
		}
		defer f.Close()

		if _, err = io.Copy(hash, f); err != nil {
			return errors.Wrap(err, "read asset file")
		}

		a.store[path] = fmt.Sprintf("%x", hash.Sum(nil))
		return nil
	})
}
