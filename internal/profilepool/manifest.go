package profilepool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Manifest struct {
	Version             int       `json:"version"`
	ProfileID           string    `json:"profile_id"`
	Provider            string    `json:"provider"`
	DesiredState        State     `json:"desired_state"`
	EffectiveSamples    int       `json:"effective_samples"`
	SuccessWeight       float64   `json:"success_weight"`
	FailureWeight       float64   `json:"failure_weight"`
	RecentEWMA          float64   `json:"recent_ewma"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	Generation          uint64    `json:"generation"`
	QuarantinedUntil    time.Time `json:"quarantined_until,omitempty"`
	RecoveryAttempts    int       `json:"recovery_attempts"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type ManifestStore struct{ root string }

func NewManifestStore(root string) (*ManifestStore, error) {
	if root == "" {
		return nil, fmt.Errorf("profile manifest root is empty")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &ManifestStore{root: root}, nil
}

func (store *ManifestStore) Load(profileID string) (Manifest, bool, error) {
	data, err := os.ReadFile(store.path(profileID))
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, false, nil
	}
	if err != nil {
		return Manifest{}, false, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, false, err
	}
	if manifest.Version != 1 || manifest.ProfileID != profileID {
		return Manifest{}, false, fmt.Errorf("invalid manifest for %q", profileID)
	}
	return manifest, true, nil
}

func (store *ManifestStore) Save(manifest Manifest) error {
	manifest.Version = 1
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	target := store.path(manifest.ProfileID)
	temporary, err := os.CreateTemp(store.root, ".manifest-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temporary.Name()
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, target); err != nil {
		return err
	}
	directory, err := os.Open(store.root)
	if err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func (store *ManifestStore) path(profileID string) string {
	return filepath.Join(store.root, profileID+".json")
}
