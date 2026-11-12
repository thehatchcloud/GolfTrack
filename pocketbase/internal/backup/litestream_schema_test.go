package backup

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

// The structs below mirror the parts of Litestream's config file that Render
// writes, with the YAML keys copied from Litestream v0.5.17
// (cmd/litestream/main.go and server.go). Litestream itself reads the file
// with gopkg.in/yaml.v2, so parsing each rendered config strictly with the
// same library catches YAML it would misread and any key it doesn't know —
// a misspelt key is otherwise silently ignored by Litestream.

type lsConfig struct {
	DBs       []lsDB     `yaml:"dbs"`
	Retention *lsEnabled `yaml:"retention"`
	Socket    *lsSocket  `yaml:"socket"`
}

type lsDB struct {
	Path    string     `yaml:"path"`
	Replica *lsReplica `yaml:"replica"`
}

type lsEnabled struct {
	Enabled *bool `yaml:"enabled"`
}

type lsSocket struct {
	Enabled     bool   `yaml:"enabled"`
	Path        string `yaml:"path"`
	Permissions uint32 `yaml:"permissions"`
}

type lsReplica struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`

	// S3
	AccessKeyID     string `yaml:"access-key-id"`
	SecretAccessKey string `yaml:"secret-access-key"`
	Region          string `yaml:"region"`
	Bucket          string `yaml:"bucket"`
	Endpoint        string `yaml:"endpoint"`
	ForcePathStyle  *bool  `yaml:"force-path-style"`
	SkipVerify      bool   `yaml:"skip-verify"`

	// ABS
	AccountName string `yaml:"account-name"`
	AccountKey  string `yaml:"account-key"`
	SASToken    string `yaml:"sas-token"`

	// SFTP
	Host     string `yaml:"host"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	KeyPath  string `yaml:"key-path"`
	HostKey  string `yaml:"host-key"`
}

func TestRenderedConfigParsesAsLitestream(t *testing.T) {
	priv, pub := sshKeys(t)
	cases := []Config{
		{Kind: KindFile, File: &FileConfig{Path: "/backup"}},
		{Kind: KindSFTP, SFTP: &SFTPConfig{Host: "h", User: "u", Path: "/p", Password: `a$b"c\d: #e`, HostKey: pub}},
		{Kind: KindSFTP, SFTP: &SFTPConfig{Host: "h", User: "u", Path: "/p", PrivateKey: priv}},
		{Kind: KindS3, KeepOldBackups: true, S3: &S3Config{Provider: ProviderOther, Bucket: "b", Endpoint: "https://m:9000",
			ForcePathStyle: true, SkipVerify: true, AccessKeyID: "AK", SecretAccessKey: "s:e c'r\"e\\t\n$x"}},
		{Kind: KindGCS, GCS: &GCSConfig{Bucket: "b", ServiceAccountJSON: serviceAccountJSON}},
		{Kind: KindABS, ABS: &ABSConfig{AccountName: "a", Container: "c", AccountKey: "k=="}},
		{Kind: KindABS, ABS: &ABSConfig{AccountName: "a", Container: "c", SASToken: "sv=1&sig=%2B"}},
	}
	for _, cfg := range cases {
		t.Run(string(cfg.Kind), func(t *testing.T) {
			cfg.Normalize()
			r, err := Render(cfg, "/data", "/data/backup/run")
			if err != nil {
				t.Fatal(err)
			}
			var got lsConfig
			if err := yaml.UnmarshalStrict(r.Files[RenderedConfigFile], &got); err != nil {
				t.Fatalf("Litestream's YAML parser rejects the config: %v\n%s", err, r.Files[RenderedConfigFile])
			}

			if len(got.DBs) != len(Databases) {
				t.Fatalf("got %d databases, want %d", len(got.DBs), len(Databases))
			}
			for i, db := range got.DBs {
				if db.Replica == nil || db.Replica.Type != string(cfg.Kind) {
					t.Fatalf("db %d replica = %+v, want type %s", i, db.Replica, cfg.Kind)
				}
				if !strings.HasSuffix(db.Replica.Path, "/"+Databases[i].Replica) {
					t.Fatalf("db %d replica path = %q", i, db.Replica.Path)
				}
			}
			// Secrets must survive quoting byte for byte.
			rep := got.DBs[0].Replica
			switch cfg.Kind {
			case KindSFTP:
				if rep.Password != cfg.SFTP.Password {
					t.Fatalf("password = %q, want %q", rep.Password, cfg.SFTP.Password)
				}
			case KindS3:
				if rep.SecretAccessKey != cfg.S3.SecretAccessKey {
					t.Fatalf("secret = %q, want %q", rep.SecretAccessKey, cfg.S3.SecretAccessKey)
				}
				if got.Retention == nil || got.Retention.Enabled == nil || *got.Retention.Enabled {
					t.Fatalf("retention = %+v, want enabled: false", got.Retention)
				}
			case KindABS:
				if rep.SASToken != cfg.ABS.SASToken || rep.AccountKey != cfg.ABS.AccountKey {
					t.Fatalf("azure auth = %q/%q", rep.AccountKey, rep.SASToken)
				}
			}
			if got.Socket == nil || !got.Socket.Enabled || got.Socket.Permissions != 0o600 {
				t.Fatalf("socket = %+v", got.Socket)
			}
		})
	}
}
