// Package librarycache defines the persistent library snapshot contract.
package librarycache

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"

	"github.com/lunguini/smolify/internal/model"
)

const version = 1

// ErrNotExist identifies an account without a stored snapshot.
var ErrNotExist = fs.ErrNotExist

// Snapshot contains only complete remote listings.
type Snapshot struct {
	Playlists []model.Playlist         `json:"playlists"`
	Listings  map[string]model.Listing `json:"listings"`
}

type document struct {
	Version int      `json:"version"`
	Account string   `json:"account"`
	Data    Snapshot `json:"data"`
}

// Cache is the account-scoped library cache used by the app.
type Cache interface {
	Load(account string) (Snapshot, error)
	Generation(account string) uint64
	Save(account string, generation, revision uint64, data Snapshot) error
	PreparePurge(account string) func() error
	Purge(account string) error
	PurgeAll() error
}

func accountKey(account string) string {
	h := sha256.Sum256([]byte(account))
	return hex.EncodeToString(h[:])
}
