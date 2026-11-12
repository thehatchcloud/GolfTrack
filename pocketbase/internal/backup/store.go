package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DirName is the folder under the PocketBase data directory that holds the
// backup setting and, while Litestream runs, its rendered config.
const DirName = "backup"

// configFile is the saved setting, inside Dir.
const configFile = "config.json"

// ErrSecretsKeyRequired is returned by Save for a config with credentials
// when no usable GOLFTRACK_SECRETS_KEY is configured. Credentials are never
// written to disk unencrypted.
var ErrSecretsKeyRequired = errors.New("set " + EnvSecretsKey + " on the server to store backup credentials")

// Source says where the effective backup setting came from.
type Source string

const (
	SourceUI   Source = "ui"   // saved by an administrator on the settings page
	SourceEnv  Source = "env"  // the LITESTREAM_* environment variables
	SourceNone Source = "none" // neither: backups are off
)

// Store reads and writes the saved backup setting.
type Store struct {
	// Dir is <data dir>/backup.
	Dir string

	// Sealer encrypts credentials. Nil when GOLFTRACK_SECRETS_KEY is unset
	// or invalid; KeyErr then says why.
	Sealer *Sealer
	KeyErr error
}

// NewStore returns the store for a PocketBase data directory, with the
// encryption key read from the environment.
func NewStore(dataDir string) *Store {
	sealer, err := LoadKey()
	return &Store{Dir: filepath.Join(dataDir, DirName), Sealer: sealer, KeyErr: err}
}

// Effective is the backup setting to run, and why.
type Effective struct {
	Config Config
	Source Source

	// NeedsCredentials is set when the saved setting's credentials could not
	// be decrypted (the key changed or is missing). Backups stay paused
	// until an administrator enters them again: the admin chose this
	// destination, so the app must not quietly fall back to the
	// environment's.
	NeedsCredentials bool
}

// Effective resolves the setting to run. A setting saved on the page wins
// over the environment — including a saved "off" (owner decision on #172).
// The LITESTREAM_* variables apply only when nothing has been saved.
func (s *Store) Effective() (Effective, error) {
	cfg, found, needsCreds, err := s.load()
	if err != nil {
		return Effective{}, err
	}
	if found {
		return Effective{Config: cfg, Source: SourceUI, NeedsCredentials: needsCreds}, nil
	}
	if env, ok := FromEnv(); ok {
		return Effective{Config: env, Source: SourceEnv}, nil
	}
	return Effective{Config: Config{Kind: KindNone}, Source: SourceNone}, nil
}

// Saved returns the setting saved on the page, if there is one, with its
// secrets decrypted where possible. found is false when nothing is saved.
func (s *Store) Saved() (cfg Config, found, needsCredentials bool, err error) {
	return s.load()
}

func (s *Store) load() (cfg Config, found, needsCredentials bool, err error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, configFile))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, false, false, nil
	}
	if err != nil {
		return Config{}, false, false, fmt.Errorf("read backup setting: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, false, false, fmt.Errorf("parse backup setting %s: %w", filepath.Join(s.Dir, configFile), err)
	}
	cfg.Normalize()

	for name, p := range cfg.secretFields() {
		if *p == "" {
			continue
		}
		if s.Sealer == nil {
			needsCredentials = true
			*p = ""
			continue
		}
		plain, err := s.Sealer.Open(name, *p)
		if err != nil {
			needsCredentials = true
			*p = ""
			continue
		}
		*p = plain
	}
	return cfg, true, needsCredentials, nil
}

// Save writes the setting, encrypting its credentials. The file is replaced
// atomically, so a crash mid-write leaves the previous setting in place.
func (s *Store) Save(cfg Config) error {
	cfg = cfg.Clone()
	if cfg.HasSecrets() && s.Sealer == nil {
		return ErrSecretsKeyRequired
	}
	for name, p := range cfg.secretFields() {
		if *p == "" {
			continue
		}
		sealed, err := s.Sealer.Seal(name, *p)
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", name, err)
		}
		*p = sealed
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", s.Dir, err)
	}
	return writeFileAtomic(filepath.Join(s.Dir, configFile), append(data, '\n'))
}

// Reset deletes the saved setting, so the environment's (or none) applies
// again.
func (s *Store) Reset() error {
	err := os.Remove(filepath.Join(s.Dir, configFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// writeFileAtomic writes data to path with mode 0600 via a temporary file in
// the same directory and a rename.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
