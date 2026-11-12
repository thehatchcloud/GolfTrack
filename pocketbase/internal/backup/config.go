// Package backup owns GolfTrack's backup setting: where Litestream replicates
// the SQLite databases to, and with which credentials (#172).
//
// It is data only — the model, its validation, encrypted storage on the data
// volume, the LITESTREAM_* environment fallback, and rendering a Litestream
// config file. Running Litestream is the supervisor's job (#174) and the HTTP
// routes are #175; neither lives here.
//
// The setting is deliberately not a PocketBase collection. A server whose
// volume was wiped must be able to find its backup *before* data.db exists,
// so the setting sits in a file next to the databases rather than in them,
// and pb_schema.json is untouched.
package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Kind is the type of backup destination. The values that name a Litestream
// replica type are spelled the way Litestream spells them.
type Kind string

const (
	KindNone Kind = "none" // backups off
	KindFile Kind = "file" // a directory on this server (another disk, mounted in)
	KindSFTP Kind = "sftp" // another server, over SSH
	KindS3   Kind = "s3"   // AWS S3 and S3-compatible stores
	KindGCS  Kind = "gs"   // Google Cloud Storage
	KindABS  Kind = "abs"  // Azure Blob Storage
)

// Kinds lists every Kind, in the order the settings page offers them.
var Kinds = []Kind{KindNone, KindFile, KindSFTP, KindS3, KindGCS, KindABS}

// S3 provider presets. They only change defaults and validation; Litestream
// sees an ordinary s3 replica either way.
const (
	ProviderAWS          = "aws"
	ProviderDigitalOcean = "digitalocean"
	ProviderBackblaze    = "backblaze"
	ProviderOther        = "other" // MinIO and any other S3-compatible store
)

// DefaultPathPrefix is the prefix object-store replicas live under, matching
// the layout litestream.yml has always used (pocketbase/data,
// pocketbase/auxiliary), so existing replicas carry on where they are.
const DefaultPathPrefix = "pocketbase"

// Config is one backup destination. Exactly the sub-struct named by Kind is
// set; the others are nil.
//
// Fields tagged as secrets in secretFields are encrypted when stored and
// never leave the server once saved — see Redacted.
type Config struct {
	Kind Kind `json:"kind"`

	File *FileConfig `json:"file,omitempty"`
	SFTP *SFTPConfig `json:"sftp,omitempty"`
	S3   *S3Config   `json:"s3,omitempty"`
	GCS  *GCSConfig  `json:"gs,omitempty"`
	ABS  *ABSConfig  `json:"abs,omitempty"`

	// KeepOldBackups renders Litestream's `retention.enabled: false`: nothing
	// is ever deleted at the destination, so the credentials need no delete
	// permission. Old files are cleaned up by the provider's lifecycle rules.
	KeepOldBackups bool `json:"keep_old_backups,omitempty"`

	UpdatedAt time.Time `json:"updated_at,omitzero"`
	UpdatedBy string    `json:"updated_by,omitempty"` // users record id
}

// FileConfig is a directory on this server.
type FileConfig struct {
	Path string `json:"path"`
}

// SFTPConfig is a directory on another server, reached over SSH.
type SFTPConfig struct {
	Host string `json:"host"`
	Port int    `json:"port,omitempty"` // default 22
	User string `json:"user"`
	Path string `json:"path"`

	// Exactly one of Password and PrivateKey. Both are secrets.
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`

	// HostKey is the server's public key in authorized_keys form
	// ("ssh-ed25519 AAAA…"). Optional, but without it Litestream does not
	// check it is talking to the right server.
	HostKey string `json:"host_key,omitempty"`
}

// S3Config is an S3 or S3-compatible bucket.
type S3Config struct {
	Provider       string `json:"provider,omitempty"`
	Bucket         string `json:"bucket"`
	Path           string `json:"path,omitempty"`
	Region         string `json:"region,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	ForcePathStyle bool   `json:"force_path_style,omitempty"`
	SkipVerify     bool   `json:"skip_verify,omitempty"`

	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"` // secret
}

// GCSConfig is a Google Cloud Storage bucket.
type GCSConfig struct {
	Bucket             string `json:"bucket"`
	Path               string `json:"path,omitempty"`
	ServiceAccountJSON string `json:"service_account_json,omitempty"` // secret
}

// ABSConfig is an Azure Blob Storage container.
type ABSConfig struct {
	AccountName string `json:"account_name"`
	Container   string `json:"container"`
	Path        string `json:"path,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`

	// Exactly one of AccountKey and SASToken. Both are secrets.
	AccountKey string `json:"account_key,omitempty"`
	SASToken   string `json:"sas_token,omitempty"`
}

// secretFields returns pointers to every secret field the config has, keyed
// by a stable name. It is the one list encryption, redaction and merging all
// work from, so a new secret field cannot be encrypted but leaked, or the
// reverse.
func (c *Config) secretFields() map[string]*string {
	f := map[string]*string{}
	if c.SFTP != nil {
		f["sftp.password"] = &c.SFTP.Password
		f["sftp.private_key"] = &c.SFTP.PrivateKey
	}
	if c.S3 != nil {
		f["s3.secret_access_key"] = &c.S3.SecretAccessKey
	}
	if c.GCS != nil {
		f["gs.service_account_json"] = &c.GCS.ServiceAccountJSON
	}
	if c.ABS != nil {
		f["abs.account_key"] = &c.ABS.AccountKey
		f["abs.sas_token"] = &c.ABS.SASToken
	}
	return f
}

// HasSecrets reports whether any secret field is set.
func (c *Config) HasSecrets() bool {
	for _, p := range c.secretFields() {
		if *p != "" {
			return true
		}
	}
	return false
}

// Clone returns a deep copy.
func (c Config) Clone() Config {
	if c.File != nil {
		v := *c.File
		c.File = &v
	}
	if c.SFTP != nil {
		v := *c.SFTP
		c.SFTP = &v
	}
	if c.S3 != nil {
		v := *c.S3
		c.S3 = &v
	}
	if c.GCS != nil {
		v := *c.GCS
		c.GCS = &v
	}
	if c.ABS != nil {
		v := *c.ABS
		c.ABS = &v
	}
	return c
}

// Redacted returns a copy safe to send to a browser: every secret blanked,
// plus the names of the secrets that were set, so the page can show
// "Saved — leave blank to keep".
func (c Config) Redacted() (Config, []string) {
	out := c.Clone()
	var set []string
	for name, p := range out.secretFields() {
		if *p != "" {
			set = append(set, name)
			*p = ""
		}
	}
	sort.Strings(set)
	return out, set
}

// KeepSecretsFrom fills blank secret fields from prev, when prev is the same
// kind of destination. A form that leaves a saved secret blank means "keep
// it", not "erase it".
//
// SFTP (password or private key) and Azure (account key or SAS token) take one
// of two alternatives. If the form supplied either one, that is the admin
// choosing it, so neither half is copied back from prev.
func (c *Config) KeepSecretsFrom(prev Config) {
	if prev.Kind != c.Kind {
		return
	}
	old := prev.secretFields()
	cur := c.secretFields()
	for _, group := range secretGroups {
		chosen := false
		for _, name := range group {
			if p := cur[name]; p != nil && *p != "" {
				chosen = true
			}
		}
		if chosen {
			continue
		}
		for _, name := range group {
			if p, o := cur[name], old[name]; p != nil && o != nil {
				*p = *o
			}
		}
	}
}

// secretGroups lists the secret fields, with alternatives grouped together.
var secretGroups = [][]string{
	{"sftp.password", "sftp.private_key"},
	{"s3.secret_access_key"},
	{"gs.service_account_json"},
	{"abs.account_key", "abs.sas_token"},
}

// FieldErrors maps a form field ("s3.bucket") to what is wrong with it.
type FieldErrors map[string]string

func (e FieldErrors) Error() string {
	keys := make([]string, 0, len(e))
	for k := range e {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + e[k]
	}
	return "invalid backup setting: " + strings.Join(parts, "; ")
}

// Normalize trims whitespace, fills defaults and drops the sub-structs Kind
// does not use. Validate expects a normalized config.
func (c *Config) Normalize() {
	c.Kind = Kind(strings.ToLower(strings.TrimSpace(string(c.Kind))))
	if c.Kind == "" {
		c.Kind = KindNone
	}
	if c.Kind != KindFile {
		c.File = nil
	}
	if c.Kind != KindSFTP {
		c.SFTP = nil
	}
	if c.Kind != KindS3 {
		c.S3 = nil
	}
	if c.Kind != KindGCS {
		c.GCS = nil
	}
	if c.Kind != KindABS {
		c.ABS = nil
	}

	switch {
	case c.File != nil:
		c.File.Path = cleanPath(c.File.Path)
	case c.SFTP != nil:
		s := c.SFTP
		s.Host, s.User = strings.TrimSpace(s.Host), strings.TrimSpace(s.User)
		s.Path = strings.TrimSpace(s.Path)
		s.PrivateKey = strings.TrimSpace(s.PrivateKey)
		s.HostKey = normalizeHostKey(s.HostKey)
		if s.Port == 0 {
			s.Port = 22
		}
	case c.S3 != nil:
		s := c.S3
		s.Provider = strings.ToLower(strings.TrimSpace(s.Provider))
		if s.Provider == "" {
			s.Provider = ProviderOther
		}
		s.Bucket, s.Region = strings.TrimSpace(s.Bucket), strings.TrimSpace(s.Region)
		s.Endpoint = strings.TrimRight(strings.TrimSpace(s.Endpoint), "/")
		s.Path = cleanPrefix(s.Path)
		s.AccessKeyID = strings.TrimSpace(s.AccessKeyID)
		s.SecretAccessKey = strings.TrimSpace(s.SecretAccessKey)
		switch s.Provider {
		case ProviderAWS:
			s.ForcePathStyle, s.SkipVerify = false, false
		case ProviderDigitalOcean:
			// DO Spaces rejects path-style requests (pocketbase/DEPLOYMENT.md).
			s.ForcePathStyle, s.SkipVerify = false, false
			if s.Endpoint == "" && s.Region != "" {
				s.Endpoint = "https://" + s.Region + ".digitaloceanspaces.com"
			}
		case ProviderBackblaze:
			// Litestream detects B2 endpoints and sets path-style itself.
			s.SkipVerify = false
		case ProviderOther:
			if s.Region == "" {
				// Litestream requires a region even where the store ignores
				// it (MinIO).
				s.Region = "us-east-1"
			}
		}
	case c.GCS != nil:
		c.GCS.Bucket = strings.TrimSpace(c.GCS.Bucket)
		c.GCS.Path = cleanPrefix(c.GCS.Path)
		c.GCS.ServiceAccountJSON = strings.TrimSpace(c.GCS.ServiceAccountJSON)
	case c.ABS != nil:
		a := c.ABS
		a.AccountName, a.Container = strings.TrimSpace(a.AccountName), strings.TrimSpace(a.Container)
		a.Endpoint = strings.TrimSpace(a.Endpoint)
		a.Path = cleanPrefix(a.Path)
		a.AccountKey = strings.TrimSpace(a.AccountKey)
		a.SASToken = strings.TrimPrefix(strings.TrimSpace(a.SASToken), "?")
	}
}

// Validate checks a normalized config. dataDir is the PocketBase data
// directory: a local backup inside it would sit on the same volume as the
// database it is meant to protect.
//
// A local destination is also checked on disk (it must exist and be
// writable), because the usual mistake is a missing volume mount and the
// admin should hear about it when saving, not from a failing backup later.
func (c *Config) Validate(dataDir string) error {
	errs := FieldErrors{}
	req := func(field, v string) {
		if v == "" {
			errs[field] = "is required"
		}
	}

	switch c.Kind {
	case KindNone:
	case KindFile:
		if c.File == nil {
			c.File = &FileConfig{}
		}
		validateFilePath(errs, c.File.Path, dataDir)
	case KindSFTP:
		if c.SFTP == nil {
			c.SFTP = &SFTPConfig{}
		}
		s := c.SFTP
		req("sftp.host", s.Host)
		req("sftp.user", s.User)
		req("sftp.path", s.Path)
		if strings.ContainsAny(s.Host, ":/@ ") {
			errs["sftp.host"] = "must be a host name or address only — put the port in the port field"
		}
		if s.Port < 1 || s.Port > 65535 {
			errs["sftp.port"] = "must be between 1 and 65535"
		}
		switch {
		case s.Password == "" && s.PrivateKey == "":
			errs["sftp.password"] = "enter a password or a private key"
		case s.Password != "" && s.PrivateKey != "":
			errs["sftp.private_key"] = "use either a password or a private key, not both"
		case s.PrivateKey != "":
			if _, err := ssh.ParseRawPrivateKey([]byte(s.PrivateKey)); err != nil {
				var missing *ssh.PassphraseMissingError
				if errors.As(err, &missing) {
					errs["sftp.private_key"] = "must not be protected by a passphrase"
				} else {
					errs["sftp.private_key"] = "is not a valid SSH private key"
				}
			}
		}
		if s.HostKey != "" {
			if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(s.HostKey)); err != nil {
				errs["sftp.host_key"] = "must be a public key like the output of ssh-keyscan (ssh-ed25519 AAAA…)"
			}
		}
	case KindS3:
		if c.S3 == nil {
			c.S3 = &S3Config{}
		}
		s := c.S3
		req("s3.bucket", s.Bucket)
		req("s3.region", s.Region)
		req("s3.access_key_id", s.AccessKeyID)
		req("s3.secret_access_key", s.SecretAccessKey)
		switch s.Provider {
		case ProviderAWS, ProviderDigitalOcean, ProviderBackblaze, ProviderOther:
		default:
			errs["s3.provider"] = "is not a supported provider"
		}
		if s.Provider != ProviderAWS {
			req("s3.endpoint", s.Endpoint)
		}
		if s.Endpoint != "" {
			if msg := endpointProblem(s.Endpoint, s.Bucket); msg != "" {
				errs["s3.endpoint"] = msg
			}
		}
	case KindGCS:
		if c.GCS == nil {
			c.GCS = &GCSConfig{}
		}
		req("gs.bucket", c.GCS.Bucket)
		req("gs.service_account_json", c.GCS.ServiceAccountJSON)
		if c.GCS.ServiceAccountJSON != "" && !isServiceAccountJSON(c.GCS.ServiceAccountJSON) {
			errs["gs.service_account_json"] = "must be a service-account key file (JSON with type \"service_account\")"
		}
	case KindABS:
		if c.ABS == nil {
			c.ABS = &ABSConfig{}
		}
		a := c.ABS
		req("abs.account_name", a.AccountName)
		req("abs.container", a.Container)
		switch {
		case a.AccountKey == "" && a.SASToken == "":
			errs["abs.account_key"] = "enter an account key or a SAS token"
		case a.AccountKey != "" && a.SASToken != "":
			errs["abs.sas_token"] = "use either an account key or a SAS token, not both"
		}
		if a.Endpoint != "" {
			if u, err := url.Parse(a.Endpoint); err != nil || u.Scheme != "https" || u.Host == "" {
				errs["abs.endpoint"] = "must be an https:// URL"
			}
		}
	default:
		errs["kind"] = "is not a supported backup type"
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func validateFilePath(errs FieldErrors, p, dataDir string) {
	switch {
	case p == "":
		errs["file.path"] = "is required"
		return
	case !filepath.IsAbs(p):
		errs["file.path"] = "must be an absolute path, such as /backup"
		return
	}
	if dataDir != "" && within(p, cleanPath(dataDir)) {
		errs["file.path"] = "must not be inside the data directory — a backup on the same disk as the database won't survive losing that disk"
		return
	}
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		errs["file.path"] = "does not exist on the server — check the volume is mounted into the container"
		return
	}
	probe, err := os.CreateTemp(p, ".golftrack-write-test-*")
	if err != nil {
		errs["file.path"] = "is not writable by the app (uid 1001) — check its owner and permissions"
		return
	}
	probe.Close()
	os.Remove(probe.Name())
}

// endpointProblem catches the endpoint mistakes that make every request fail.
func endpointProblem(endpoint, bucket string) string {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "must be a URL starting with https://"
	}
	if u.Path != "" && u.Path != "/" {
		return "must be the server's address only, without a path"
	}
	// The DO Spaces gotcha: the dashboard shows the bucket-qualified URL, but
	// Litestream wants the region root and adds the bucket itself.
	if bucket != "" && strings.HasPrefix(strings.ToLower(u.Hostname()), strings.ToLower(bucket)+".") {
		return "must be the region address (such as https://nyc3.digitaloceanspaces.com), not the bucket's own URL"
	}
	return ""
}

func isServiceAccountJSON(s string) bool {
	var v struct {
		Type        string `json:"type"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
	}
	return json.Unmarshal([]byte(s), &v) == nil &&
		v.Type == "service_account" && v.ClientEmail != "" && v.PrivateKey != ""
}

// normalizeHostKey accepts a line straight from ssh-keyscan
// ("host ssh-ed25519 AAAA…") and keeps just the key ("ssh-ed25519 AAAA…").
func normalizeHostKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "#") {
		return ""
	}
	fields := strings.Fields(s)
	if len(fields) >= 3 && !looksLikeKeyType(fields[0]) && looksLikeKeyType(fields[1]) {
		fields = fields[1:]
	}
	if len(fields) > 2 {
		fields = fields[:2] // drop a trailing comment
	}
	return strings.Join(fields, " ")
}

func looksLikeKeyType(s string) bool {
	return strings.HasPrefix(s, "ssh-") || strings.HasPrefix(s, "ecdsa-") || strings.HasPrefix(s, "sk-")
}

func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

func cleanPrefix(p string) string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return DefaultPathPrefix
	}
	return p
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

// Summary is a one-line, secret-free description of the destination, for
// logs and the status panel.
func (c Config) Summary() string {
	if !c.complete() {
		return string(c.Kind) + " (incomplete)"
	}
	switch c.Kind {
	case KindFile:
		return "local directory " + c.File.Path
	case KindSFTP:
		return fmt.Sprintf("sftp://%s@%s:%d%s", c.SFTP.User, c.SFTP.Host, c.SFTP.Port, c.SFTP.Path)
	case KindS3:
		if c.S3.Endpoint != "" {
			return fmt.Sprintf("s3://%s/%s at %s", c.S3.Bucket, c.S3.Path, c.S3.Endpoint)
		}
		return fmt.Sprintf("s3://%s/%s", c.S3.Bucket, c.S3.Path)
	case KindGCS:
		return fmt.Sprintf("gs://%s/%s", c.GCS.Bucket, c.GCS.Path)
	case KindABS:
		return fmt.Sprintf("abs://%s/%s/%s", c.ABS.AccountName, c.ABS.Container, c.ABS.Path)
	default:
		return "off"
	}
}

// complete reports whether the sub-struct Kind names is present. A config
// loaded from a damaged or hand-edited file may lack it.
func (c Config) complete() bool {
	switch c.Kind {
	case KindFile:
		return c.File != nil
	case KindSFTP:
		return c.SFTP != nil
	case KindS3:
		return c.S3 != nil
	case KindGCS:
		return c.GCS != nil
	case KindABS:
		return c.ABS != nil
	}
	return true
}
