package backup

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

// testKey is a fixed AES-256 key for tests.
var testKey = []byte("0123456789abcdef0123456789abcdef")

func testSealer(t *testing.T) *Sealer {
	t.Helper()
	s, err := NewSealer(testKey)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// clearEnv blanks every variable this package reads, so a developer's or
// CI's own environment can't leak into a test.
func clearEnv(t *testing.T) {
	for _, k := range []string{EnvBucket, EnvEndpoint, EnvAccessKeyID, EnvSecretAccessKey, EnvSecretsKey} {
		t.Setenv(k, "")
	}
}

// sshKeys returns an unencrypted OpenSSH private key and the matching
// authorized_keys line.
func sshKeys(t *testing.T) (private, public string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block)), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
}

const serviceAccountJSON = `{"type":"service_account","client_email":"litestream@proj.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"}`

// ---- Validation ----

func TestValidateAcceptsEachKind(t *testing.T) {
	priv, pub := sshKeys(t)
	backupDir := t.TempDir()
	dataDir := t.TempDir()

	cases := map[string]Config{
		"none": {Kind: KindNone},
		"file": {Kind: KindFile, File: &FileConfig{Path: backupDir}},
		"sftp password": {Kind: KindSFTP, SFTP: &SFTPConfig{
			Host: "backup.example.com", User: "golftrack", Path: "/srv/golftrack", Password: "pw",
		}},
		"sftp key + keyscan host key": {Kind: KindSFTP, SFTP: &SFTPConfig{
			Host: "backup.example.com", User: "golftrack", Path: "/srv/golftrack",
			PrivateKey: priv, HostKey: "backup.example.com " + pub,
		}},
		"aws": {Kind: KindS3, S3: &S3Config{
			Provider: ProviderAWS, Bucket: "b", Region: "us-east-1", AccessKeyID: "AK", SecretAccessKey: "SK",
		}},
		"digitalocean fills endpoint from region": {Kind: KindS3, S3: &S3Config{
			Provider: ProviderDigitalOcean, Bucket: "b", Region: "nyc3", AccessKeyID: "AK", SecretAccessKey: "SK",
		}},
		"minio fills region": {Kind: KindS3, S3: &S3Config{
			Provider: ProviderOther, Bucket: "b", Endpoint: "https://minio.example.com:9000", AccessKeyID: "AK", SecretAccessKey: "SK",
		}},
		"gcs":       {Kind: KindGCS, GCS: &GCSConfig{Bucket: "b", ServiceAccountJSON: serviceAccountJSON}},
		"azure key": {Kind: KindABS, ABS: &ABSConfig{AccountName: "acct", Container: "c", AccountKey: "k"}},
		"azure sas": {Kind: KindABS, ABS: &ABSConfig{AccountName: "acct", Container: "c", SASToken: "?sv=1&sig=x"}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			cfg.Normalize()
			if err := cfg.Validate(dataDir); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	priv, _ := sshKeys(t)
	dataDir := t.TempDir()
	insideData := filepath.Join(dataDir, "backups")
	if err := os.Mkdir(insideData, 0o700); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		cfg   Config
		field string
		want  string
	}{
		{"unknown kind", Config{Kind: "ftp"}, "kind", "not a supported"},
		{"file relative", Config{Kind: KindFile, File: &FileConfig{Path: "backup"}}, "file.path", "absolute"},
		{"file inside data dir", Config{Kind: KindFile, File: &FileConfig{Path: insideData}}, "file.path", "data directory"},
		{"file is the data dir", Config{Kind: KindFile, File: &FileConfig{Path: dataDir}}, "file.path", "data directory"},
		{"file missing", Config{Kind: KindFile, File: &FileConfig{Path: "/nonexistent/golftrack-backup"}}, "file.path", "mounted"},
		{"sftp no auth", Config{Kind: KindSFTP, SFTP: &SFTPConfig{Host: "h", User: "u", Path: "/p"}}, "sftp.password", "password or a private key"},
		{"sftp both auth", Config{Kind: KindSFTP, SFTP: &SFTPConfig{Host: "h", User: "u", Path: "/p", Password: "x", PrivateKey: priv}}, "sftp.private_key", "not both"},
		{"sftp bad key", Config{Kind: KindSFTP, SFTP: &SFTPConfig{Host: "h", User: "u", Path: "/p", PrivateKey: "nope"}}, "sftp.private_key", "not a valid"},
		{"sftp host with port", Config{Kind: KindSFTP, SFTP: &SFTPConfig{Host: "h:22", User: "u", Path: "/p", Password: "x"}}, "sftp.host", "port field"},
		{"sftp bad port", Config{Kind: KindSFTP, SFTP: &SFTPConfig{Host: "h", Port: 70000, User: "u", Path: "/p", Password: "x"}}, "sftp.port", "between"},
		{"sftp bad host key", Config{Kind: KindSFTP, SFTP: &SFTPConfig{Host: "h", User: "u", Path: "/p", Password: "x", HostKey: "SHA256:abc"}}, "sftp.host_key", "ssh-keyscan"},
		{"s3 no bucket", Config{Kind: KindS3, S3: &S3Config{Provider: ProviderAWS, Region: "r", AccessKeyID: "a", SecretAccessKey: "s"}}, "s3.bucket", "required"},
		{"s3 no secret", Config{Kind: KindS3, S3: &S3Config{Provider: ProviderAWS, Bucket: "b", Region: "r", AccessKeyID: "a"}}, "s3.secret_access_key", "required"},
		{"s3 compatible needs endpoint", Config{Kind: KindS3, S3: &S3Config{Provider: ProviderBackblaze, Bucket: "b", Region: "r", AccessKeyID: "a", SecretAccessKey: "s"}}, "s3.endpoint", "required"},
		{"s3 bucket-qualified endpoint", Config{Kind: KindS3, S3: &S3Config{Provider: ProviderDigitalOcean, Bucket: "golftrack-backup", Region: "nyc3",
			Endpoint: "https://golftrack-backup.nyc3.digitaloceanspaces.com", AccessKeyID: "a", SecretAccessKey: "s"}}, "s3.endpoint", "region address"},
		{"s3 endpoint not a url", Config{Kind: KindS3, S3: &S3Config{Provider: ProviderOther, Bucket: "b", Endpoint: "minio:9000", AccessKeyID: "a", SecretAccessKey: "s"}}, "s3.endpoint", "https://"},
		{"s3 endpoint with path", Config{Kind: KindS3, S3: &S3Config{Provider: ProviderOther, Bucket: "b", Endpoint: "https://minio/b", AccessKeyID: "a", SecretAccessKey: "s"}}, "s3.endpoint", "without a path"},
		{"s3 unknown provider", Config{Kind: KindS3, S3: &S3Config{Provider: "wasabi", Bucket: "b", Region: "r", Endpoint: "https://x", AccessKeyID: "a", SecretAccessKey: "s"}}, "s3.provider", "supported"},
		{"gcs not service account", Config{Kind: KindGCS, GCS: &GCSConfig{Bucket: "b", ServiceAccountJSON: `{"type":"authorized_user"}`}}, "gs.service_account_json", "service-account"},
		{"azure no auth", Config{Kind: KindABS, ABS: &ABSConfig{AccountName: "a", Container: "c"}}, "abs.account_key", "account key or a SAS token"},
		{"azure both auth", Config{Kind: KindABS, ABS: &ABSConfig{AccountName: "a", Container: "c", AccountKey: "k", SASToken: "t"}}, "abs.sas_token", "not both"},
		{"azure http endpoint", Config{Kind: KindABS, ABS: &ABSConfig{AccountName: "a", Container: "c", AccountKey: "k", Endpoint: "http://x"}}, "abs.endpoint", "https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			cfg.Normalize()
			err := cfg.Validate(dataDir)
			var fe FieldErrors
			if !errors.As(err, &fe) {
				t.Fatalf("Validate = %v, want FieldErrors", err)
			}
			if !strings.Contains(fe[tc.field], tc.want) {
				t.Fatalf("error for %s = %q, want it to mention %q (all: %v)", tc.field, fe[tc.field], tc.want, fe)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	_, pub := sshKeys(t)
	cfg := Config{
		Kind: " SFTP ",
		SFTP: &SFTPConfig{Host: " h ", User: "u", Path: "/p", HostKey: "h " + pub + " comment"},
		S3:   &S3Config{Bucket: "dropped"},
	}
	cfg.Normalize()
	if cfg.Kind != KindSFTP || cfg.S3 != nil {
		t.Fatalf("kind/unused structs not normalized: %+v", cfg)
	}
	if cfg.SFTP.Port != 22 || cfg.SFTP.Host != "h" {
		t.Fatalf("sftp defaults: %+v", cfg.SFTP)
	}
	if cfg.SFTP.HostKey != pub {
		t.Fatalf("host key = %q, want %q", cfg.SFTP.HostKey, pub)
	}

	do := Config{Kind: KindS3, S3: &S3Config{Provider: "DigitalOcean", Region: "nyc3", Path: "/x/", ForcePathStyle: true}}
	do.Normalize()
	if do.S3.Endpoint != "https://nyc3.digitaloceanspaces.com" || do.S3.ForcePathStyle || do.S3.Path != "x" {
		t.Fatalf("digitalocean preset: %+v", do.S3)
	}

	empty := Config{}
	empty.Normalize()
	if empty.Kind != KindNone {
		t.Fatalf("empty kind = %q, want none", empty.Kind)
	}
}

// ---- Secrets ----

func TestSealRoundTrip(t *testing.T) {
	s := testSealer(t)
	sealed, err := s.Seal("s3.secret_access_key", "p@$$w0rd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, sealedPrefix) || strings.Contains(sealed, "p@$$w0rd") {
		t.Fatalf("sealed value looks wrong: %q", sealed)
	}
	got, err := s.Open("s3.secret_access_key", sealed)
	if err != nil || got != "p@$$w0rd" {
		t.Fatalf("Open = %q, %v", got, err)
	}

	// Bound to its field: the same ciphertext under another field fails.
	if _, err := s.Open("abs.account_key", sealed); err == nil {
		t.Fatal("Open under a different field name succeeded")
	}

	other, _ := NewSealer([]byte("ffffffffffffffffffffffffffffffff"))
	if _, err := other.Open("s3.secret_access_key", sealed); err == nil {
		t.Fatal("Open with a different key succeeded")
	}
}

func TestLoadKey(t *testing.T) {
	clearEnv(t)
	if _, err := LoadKey(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("unset: err = %v, want ErrNoKey", err)
	}

	t.Setenv(EnvSecretsKey, base64.StdEncoding.EncodeToString(testKey))
	if _, err := LoadKey(); err != nil {
		t.Fatalf("valid key: %v", err)
	}

	t.Setenv(EnvSecretsKey, base64.StdEncoding.EncodeToString([]byte("short")))
	if _, err := LoadKey(); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("short key: err = %v", err)
	}

	t.Setenv(EnvSecretsKey, "not base64!!")
	if _, err := LoadKey(); err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("garbage key: err = %v", err)
	}
}

func TestRedactedHidesEverySecret(t *testing.T) {
	priv, _ := sshKeys(t)
	cfgs := []Config{
		{Kind: KindSFTP, SFTP: &SFTPConfig{Password: "pw1", PrivateKey: priv}},
		{Kind: KindS3, S3: &S3Config{AccessKeyID: "AK", SecretAccessKey: "sk1"}},
		{Kind: KindGCS, GCS: &GCSConfig{ServiceAccountJSON: serviceAccountJSON}},
		{Kind: KindABS, ABS: &ABSConfig{AccountKey: "ak1", SASToken: "sas1"}},
	}
	for _, cfg := range cfgs {
		red, set := cfg.Redacted()
		if red.HasSecrets() {
			t.Errorf("%s: redacted copy still has secrets: %+v", cfg.Kind, red)
		}
		if len(set) == 0 {
			t.Errorf("%s: no secret names reported", cfg.Kind)
		}
		if !cfg.HasSecrets() {
			t.Errorf("%s: Redacted modified the original", cfg.Kind)
		}
	}
}

func TestKeepSecretsFrom(t *testing.T) {
	prev := Config{Kind: KindS3, S3: &S3Config{AccessKeyID: "AK", SecretAccessKey: "old"}}

	blank := Config{Kind: KindS3, S3: &S3Config{AccessKeyID: "AK2"}}
	blank.KeepSecretsFrom(prev)
	if blank.S3.SecretAccessKey != "old" {
		t.Fatalf("blank secret not kept: %q", blank.S3.SecretAccessKey)
	}

	changed := Config{Kind: KindS3, S3: &S3Config{SecretAccessKey: "new"}}
	changed.KeepSecretsFrom(prev)
	if changed.S3.SecretAccessKey != "new" {
		t.Fatalf("new secret overwritten: %q", changed.S3.SecretAccessKey)
	}

	otherKind := Config{Kind: KindABS, ABS: &ABSConfig{}}
	otherKind.KeepSecretsFrom(prev)
	if otherKind.HasSecrets() {
		t.Fatal("secrets copied across kinds")
	}

	// Switching SFTP from password to key must not bring the password back.
	sftpPrev := Config{Kind: KindSFTP, SFTP: &SFTPConfig{Password: "pw"}}
	toKey := Config{Kind: KindSFTP, SFTP: &SFTPConfig{PrivateKey: "KEY"}}
	toKey.KeepSecretsFrom(sftpPrev)
	if toKey.SFTP.Password != "" || toKey.SFTP.PrivateKey != "KEY" {
		t.Fatalf("auth switch: %+v", toKey.SFTP)
	}
	keep := Config{Kind: KindSFTP, SFTP: &SFTPConfig{}}
	keep.KeepSecretsFrom(sftpPrev)
	if keep.SFTP.Password != "pw" {
		t.Fatalf("blank sftp auth not kept: %+v", keep.SFTP)
	}
}

// ---- Store and precedence ----

func newTestStore(t *testing.T, sealer *Sealer) *Store {
	return &Store{Dir: filepath.Join(t.TempDir(), DirName), Sealer: sealer}
}

func TestStoreSaveLoadEncrypts(t *testing.T) {
	clearEnv(t)
	st := newTestStore(t, testSealer(t))
	cfg := Config{Kind: KindS3, S3: &S3Config{Provider: ProviderAWS, Bucket: "b", Region: "r", AccessKeyID: "AK", SecretAccessKey: "very-secret"}}
	if err := st.Save(cfg); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(st.Dir, configFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "very-secret") {
		t.Fatal("secret stored in plain text")
	}
	if !strings.Contains(string(raw), `"bucket": "b"`) {
		t.Fatalf("non-secret field not readable in the file:\n%s", raw)
	}
	info, _ := os.Stat(filepath.Join(st.Dir, configFile))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, want 0600", info.Mode().Perm())
	}

	eff, err := st.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if eff.Source != SourceUI || eff.NeedsCredentials || eff.Config.S3.SecretAccessKey != "very-secret" {
		t.Fatalf("Effective = %+v", eff)
	}
}

func TestStoreRefusesSecretsWithoutKey(t *testing.T) {
	clearEnv(t)
	st := newTestStore(t, nil)
	err := st.Save(Config{Kind: KindS3, S3: &S3Config{SecretAccessKey: "s"}})
	if !errors.Is(err, ErrSecretsKeyRequired) {
		t.Fatalf("Save = %v, want ErrSecretsKeyRequired", err)
	}
	if _, err := os.Stat(filepath.Join(st.Dir, configFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a file was written anyway")
	}

	// A destination without credentials needs no key.
	if err := st.Save(Config{Kind: KindFile, File: &FileConfig{Path: "/backup"}}); err != nil {
		t.Fatalf("file destination without key: %v", err)
	}
}

func TestStoreWrongKeyNeedsCredentials(t *testing.T) {
	clearEnv(t)
	st := newTestStore(t, testSealer(t))
	if err := st.Save(Config{Kind: KindABS, ABS: &ABSConfig{AccountName: "a", Container: "c", AccountKey: "k"}}); err != nil {
		t.Fatal(err)
	}

	// Same file, different key: the setting still wins over env, but is paused.
	t.Setenv(EnvBucket, "env-bucket")
	other, _ := NewSealer([]byte("ffffffffffffffffffffffffffffffff"))
	st.Sealer = other
	eff, err := st.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if eff.Source != SourceUI || !eff.NeedsCredentials || eff.Config.ABS.AccountKey != "" {
		t.Fatalf("Effective = %+v (abs %+v)", eff, eff.Config.ABS)
	}

	// Key removed entirely: same outcome.
	st.Sealer = nil
	eff, _ = st.Effective()
	if !eff.NeedsCredentials || eff.Source != SourceUI {
		t.Fatalf("no key: Effective = %+v", eff)
	}
}

func TestEffectivePrecedence(t *testing.T) {
	clearEnv(t)
	st := newTestStore(t, testSealer(t))

	// Nothing anywhere → off.
	eff, err := st.Effective()
	if err != nil || eff.Source != SourceNone || eff.Config.Kind != KindNone {
		t.Fatalf("nothing set: %+v, %v", eff, err)
	}

	// Env only → env, with today's litestream.yml layout.
	t.Setenv(EnvBucket, "golftrack-backup")
	t.Setenv(EnvEndpoint, "https://nyc3.digitaloceanspaces.com/")
	t.Setenv(EnvAccessKeyID, "AK")
	t.Setenv(EnvSecretAccessKey, "SK")
	eff, _ = st.Effective()
	want := &S3Config{Provider: ProviderOther, Bucket: "golftrack-backup", Path: "pocketbase",
		Endpoint: "https://nyc3.digitaloceanspaces.com", AccessKeyID: "AK", SecretAccessKey: "SK"}
	if eff.Source != SourceEnv || !reflect.DeepEqual(eff.Config.S3, want) {
		t.Fatalf("env only: %+v / %+v", eff, eff.Config.S3)
	}

	// Saved setting beats env.
	if err := st.Save(Config{Kind: KindFile, File: &FileConfig{Path: "/backup"}}); err != nil {
		t.Fatal(err)
	}
	eff, _ = st.Effective()
	if eff.Source != SourceUI || eff.Config.Kind != KindFile {
		t.Fatalf("saved file over env: %+v", eff)
	}

	// A saved "off" beats env too.
	if err := st.Save(Config{Kind: KindNone}); err != nil {
		t.Fatal(err)
	}
	eff, _ = st.Effective()
	if eff.Source != SourceUI || eff.Config.Kind != KindNone {
		t.Fatalf("saved off over env: %+v", eff)
	}

	// Reset → back to env. Resetting twice is fine.
	if err := st.Reset(); err != nil {
		t.Fatal(err)
	}
	if err := st.Reset(); err != nil {
		t.Fatalf("second Reset: %v", err)
	}
	eff, _ = st.Effective()
	if eff.Source != SourceEnv {
		t.Fatalf("after reset: %+v", eff)
	}
}

// ---- Rendering ----

// TestRenderGolden pins the Litestream config for each destination type.
// Run `go test ./internal/backup -update` to rewrite testdata/ after an
// intended change, and review the diff.
func TestRenderGolden(t *testing.T) {
	const dataDir, runDir = "/data", "/data/backup/run"
	priv := "-----BEGIN OPENSSH PRIVATE KEY-----\nKEY\n-----END OPENSSH PRIVATE KEY-----"

	cases := map[string]Config{
		"file": {Kind: KindFile, File: &FileConfig{Path: "/backup"}},
		"sftp_password": {Kind: KindSFTP, SFTP: &SFTPConfig{Host: "backup.example.com", Port: 2222, User: "golftrack",
			Path: "/srv/golftrack", Password: `pa$$"word`, HostKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITEST"}},
		"sftp_key": {Kind: KindSFTP, SFTP: &SFTPConfig{Host: "backup.example.com", User: "golftrack", Path: "/srv/golftrack", PrivateKey: priv}},
		"s3_digitalocean": {Kind: KindS3, S3: &S3Config{Provider: ProviderDigitalOcean, Bucket: "golftrack-backup", Region: "nyc3",
			AccessKeyID: "AK", SecretAccessKey: "SK"}},
		"s3_minio_keep_old": {Kind: KindS3, KeepOldBackups: true, S3: &S3Config{Provider: ProviderOther, Bucket: "b",
			Endpoint: "https://minio.local:9000", ForcePathStyle: true, SkipVerify: true, AccessKeyID: "AK", SecretAccessKey: "SK"}},
		"gs":        {Kind: KindGCS, GCS: &GCSConfig{Bucket: "golftrack", Path: "prod", ServiceAccountJSON: serviceAccountJSON}},
		"abs_sas":   {Kind: KindABS, ABS: &ABSConfig{AccountName: "acct", Container: "backups", SASToken: "sv=1&sig=x"}},
		"abs_key":   {Kind: KindABS, ABS: &ABSConfig{AccountName: "acct", Container: "backups", AccountKey: "k==", Endpoint: "https://acct.blob.core.windows.net/"}},
		"env_today": envToday(t),
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if name != "env_today" {
				cfg.Normalize()
			}
			r, err := Render(cfg, dataDir, runDir)
			if err != nil {
				t.Fatal(err)
			}
			var got strings.Builder
			for _, f := range sortedKeys(r.Files) {
				got.WriteString("==> " + f + "\n")
				got.Write(r.Files[f])
				if b := r.Files[f]; len(b) > 0 && b[len(b)-1] != '\n' {
					got.WriteString("\n")
				}
			}
			for _, e := range r.Env {
				got.WriteString("==> env " + e + "\n")
			}

			golden := filepath.Join("testdata", "render_"+name+".golden")
			if *update {
				if err := os.WriteFile(golden, []byte(got.String()), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if got.String() != string(want) {
				t.Fatalf("render mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got.String(), want)
			}
		})
	}
}

// envToday is the destination production runs with now: LITESTREAM_* set,
// nothing saved. Its render must keep the bucket paths the old
// litestream.yml used.
func envToday(t *testing.T) Config {
	clearEnv(t)
	t.Setenv(EnvBucket, "golftrack-backup")
	t.Setenv(EnvEndpoint, "https://nyc3.digitaloceanspaces.com")
	t.Setenv(EnvAccessKeyID, "AK")
	t.Setenv(EnvSecretAccessKey, "SK")
	cfg, ok := FromEnv()
	if !ok {
		t.Fatal("FromEnv not ok")
	}
	return cfg
}

func TestRenderRejects(t *testing.T) {
	if _, err := Render(Config{Kind: KindNone}, "/data", "/run"); err == nil {
		t.Fatal("rendered an off config")
	}
	if _, err := Render(Config{Kind: KindFile, File: &FileConfig{Path: "/b"}}, "data", "/run"); err == nil {
		t.Fatal("rendered with a relative data dir")
	}
	incomplete := Config{Kind: KindS3}
	if _, err := Render(incomplete, "/data", "/run"); err == nil {
		t.Fatal("rendered an s3 config with no s3 settings")
	}
	if got := incomplete.Summary(); got != "s3 (incomplete)" {
		t.Fatalf("Summary = %q", got)
	}
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
