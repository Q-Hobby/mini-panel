// Package cloudprov is the list of storage the backups can be copied to, and the check of what the person types for one. It does
// not import rclone, so the panel and the helper can check a form without it; internal/remote turns checked settings into an rclone
// remote.
package cloudprov

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Field is one box of the form that connects a storage provider. Only the fields listed here ever reach rclone: some options of rclone
// run a command (sftp "ssh", "*sum_command", "server_command", webdav "bearer_token_command") or read a file of this machine
// ("key_file", "*_credentials_file"), and the copy runs as root, so a key that is not on this list is refused rather than passed on.
type Field struct {
	Key         string             `json:"key"`
	Secret      bool               `json:"secret,omitempty"`   // drawn as a password box and never shown again
	Required    bool               `json:"required,omitempty"` // the connection cannot work without it
	Multi       bool               `json:"multi,omitempty"`    // several lines, such as a private key
	Choices     []string           `json:"choices,omitempty"`
	Default     string             `json:"default,omitempty"`
	JSON        bool               `json:"json,omitempty"`        // a JSON value pasted whole, such as the token of rclone authorize
	Help        string             `json:"help,omitempty"`        // the label of a generated field: the first line of rclone's help, in English
	Placeholder string             `json:"placeholder,omitempty"` // an example rclone gives
	Check       func(string) error `json:"-"`
}

// Provider is a kind of storage the panel can connect to.
type Provider struct {
	Type   string  `json:"type"`   // the rclone backend
	Fields []Field `json:"fields"` // what the person fills in, in order
	// Fixed is written for every remote of this type and cannot be changed from the form. It keeps the memory of a 1 GB server in
	// check, and for SFTP names the commands rclone runs on the other machine (never on this one) to compute checksums.
	Fixed map[string]string `json:"-"`
	// Bucket says the path starts with a bucket (S3, B2): the form asks for it on its own line.
	Bucket bool `json:"bucket,omitempty"`
	// Generated providers come from generated.go, which tools/provgen writes from rclone's own list of options. Their labels are
	// rclone's English help, their defaults are left to rclone, and the remote may later hold other keys rclone writes itself
	// (a refreshed token, the drive it found), as long as none of them is Dangerous.
	Generated bool   `json:"generated,omitempty"`
	Name      string `json:"name,omitempty"` // what rclone calls it, such as "Microsoft OneDrive"
	// OAuth says the storage signs in through a browser. The panel runs on a server without one, so the person runs
	// `rclone authorize "<type>"` on their own computer and pastes the answer in the field "token".
	OAuth bool `json:"oauth,omitempty"`
}

var (
	hostRe    = regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$|^[0-9.]{7,15}$|^[0-9A-Fa-f:]{2,39}$`)
	userRe    = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,128}$`)
	regionRe  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	bucketRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$|^[A-Za-z0-9-]{6,50}$`)
	folderRe  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}(/[A-Za-z0-9._-]{1,64}){0,7}$`)
	tokenRe   = regexp.MustCompile(`^[\x21-\x7e]{1,512}$`)
	hostKeyRe = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)) [A-Za-z0-9+/]+={0,2}$`)
)

// checkHostKey accepts one public key in the form of a line of known_hosts without the host: "ssh-ed25519 AAAA...".
func checkHostKey(v string) error {
	if len(v) < 60 || len(v) > 1200 || !hostKeyRe.MatchString(v) {
		return errors.New("the host key is not valid; give one line such as \"ssh-ed25519 AAAA...\"")
	}
	return nil
}

func matches(re *regexp.Regexp, what string) func(string) error {
	return func(v string) error {
		if !re.MatchString(v) {
			return fmt.Errorf("%s is not valid", what)
		}
		return nil
	}
}

func checkURL(v string) error {
	if len(v) > 512 || !(strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://")) || strings.ContainsAny(v, " \"'<>\\`") {
		return errors.New("the address must start with https:// (or http://) and have no spaces or quotes")
	}
	return nil
}

func checkPort(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("the port must be a number from 1 to 65535")
	}
	return nil
}

// checkPEM accepts an OpenSSH or PEM private key without a passphrase.
func checkPEM(v string) error {
	if len(v) > 16<<10 || !strings.HasPrefix(v, "-----BEGIN ") || !strings.Contains(v, " PRIVATE KEY-----") || !strings.Contains(v, "-----END ") {
		return errors.New("paste the whole private key, from -----BEGIN to -----END")
	}
	if strings.Contains(v, "ENCRYPTED") {
		return errors.New("the private key has a passphrase; use a key without one")
	}
	return nil
}

// checkToken accepts what `rclone authorize "<type>"` prints on the person's computer, with or without the lines around it, and
// returns the token as one line of JSON, the way rclone keeps it in its config file.
func checkToken(v string) (string, error) {
	if i := strings.Index(v, "--->"); i >= 0 {
		v = v[i+4:]
	}
	if i := strings.Index(v, "<---"); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimSpace(v)
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	dec := json.NewDecoder(strings.NewReader(v))
	if err := dec.Decode(&tok); err != nil || dec.More() || tok.AccessToken == "" && tok.RefreshToken == "" {
		return "", errors.New("paste the whole answer of rclone authorize: the JSON from {\"access_token\": ... to the closing }")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(v)); err != nil {
		return "", errors.New("the token is not valid JSON")
	}
	return buf.String(), nil
}

func checkPass(v string) error {
	if len(v) > 512 {
		return errors.New("the password is too long")
	}
	return nil
}

// S3Providers are the choices of the S3 form. "Other" covers any storage that speaks S3 with an endpoint.
var S3Providers = []string{"Cloudflare", "AWS", "Wasabi", "DigitalOcean", "Linode", "Scaleway", "Hetzner", "IDrive", "BizflyCloud", "Minio", "Ceph", "Other"}

// Providers lists the storage the panel connects to, in the order the form offers them: the ones with a form made by hand first,
// then the generated ones by name.
var Providers = append(curated, generated...)

var curated = []Provider{
	{Type: "s3", Bucket: true, Fields: []Field{
		{Key: "provider", Required: true, Choices: S3Providers, Default: "Cloudflare"},
		{Key: "access_key_id", Required: true, Check: matches(tokenRe, "the access key")},
		{Key: "secret_access_key", Secret: true, Required: true, Check: matches(tokenRe, "the secret key")},
		{Key: "endpoint", Check: checkURL},
		{Key: "region", Check: matches(regionRe, "the region")},
	}, Fixed: map[string]string{"env_auth": "false", "no_check_bucket": "true", "upload_concurrency": "4", "chunk_size": "5Mi"}},
	{Type: "b2", Bucket: true, Fields: []Field{
		{Key: "account", Required: true, Check: matches(tokenRe, "the key ID")},
		{Key: "key", Secret: true, Required: true, Check: matches(tokenRe, "the application key")},
	}, Fixed: map[string]string{"hard_delete": "true", "chunk_size": "16Mi", "upload_concurrency": "2", "upload_cutoff": "16Mi"}},
	{Type: "sftp", Fields: []Field{
		{Key: "host", Required: true, Check: matches(hostRe, "the host")},
		{Key: "port", Default: "22", Check: checkPort},
		{Key: "user", Required: true, Check: matches(userRe, "the user")},
		{Key: "pass", Secret: true, Check: checkPass},
		{Key: "key_pem", Secret: true, Multi: true, Check: checkPEM},
		{Key: "host_keys", Required: true, Check: checkHostKey},
	}, Fixed: map[string]string{"shell_type": "unix", "md5sum_command": "md5sum", "sha1sum_command": "sha1sum", "set_modtime": "false", "key_use_agent": "false"}},
	{Type: "webdav", Fields: []Field{
		{Key: "url", Required: true, Check: checkURL},
		{Key: "vendor", Choices: []string{"nextcloud", "owncloud", "sharepoint", "other"}, Default: "other"},
		{Key: "user", Check: matches(userRe, "the user")},
		{Key: "pass", Secret: true, Check: checkPass},
	}},
	{Type: "ftp", Fields: []Field{
		{Key: "host", Required: true, Check: matches(hostRe, "the host")},
		{Key: "port", Default: "21", Check: checkPort},
		{Key: "user", Required: true, Check: matches(userRe, "the user")},
		{Key: "pass", Secret: true, Required: true, Check: checkPass},
		{Key: "explicit_tls", Choices: []string{"true", "false"}, Default: "true"},
	}, Fixed: map[string]string{"concurrency": "2"}},
}

// Find returns the provider of a type.
func Find(typ string) (Provider, bool) {
	for _, p := range Providers {
		if p.Type == typ {
			return p, true
		}
	}
	return Provider{}, false
}

// Settings is what the person typed for a connection: the type, the values of its fields, and where in the storage the backups go.
type Settings struct {
	Type   string            `json:"type"`
	Values map[string]string `json:"values"`
	Bucket string            `json:"bucket,omitempty"` // S3 and B2
	Folder string            `json:"folder,omitempty"` // the folder that holds mpn-backups; empty is the top
}

func noControl(v string) error {
	for _, r := range v {
		if r == '\n' || r == '\r' || r == 0 || unicode.IsControl(r) {
			return errors.New("has a line break or a control character")
		}
	}
	return nil
}

// Check validates the settings and returns the parameters to give rclone: the allowed fields that were filled in, the defaults and
// the fixed ones. A key that is not on the list of the provider is an error, never passed on. A private key comes back as one line
// with "\n" for its line ends, the way rclone reads key_pem.
func (s Settings) Check() (map[string]string, error) {
	p, ok := Find(s.Type)
	if !ok {
		return nil, fmt.Errorf("unknown storage type %q", s.Type)
	}
	allowed := map[string]Field{}
	for _, f := range p.Fields {
		allowed[f.Key] = f
	}
	keys := make([]string, 0, len(s.Values))
	for k := range s.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := map[string]string{}
	for _, k := range keys {
		f, ok := allowed[k]
		if !ok {
			return nil, fmt.Errorf("the setting %q is not accepted for %s", k, s.Type)
		}
		v := s.Values[k]
		if !f.Secret || f.Multi || f.JSON {
			v = strings.TrimSpace(strings.ReplaceAll(v, "\r\n", "\n"))
		}
		if v == "" {
			continue
		}
		if len(v) > 16<<10 {
			return nil, fmt.Errorf("%s is too long", k)
		}
		if f.JSON {
			var err error
			if v, err = checkToken(v); err != nil {
				return nil, err
			}
		}
		if f.Check != nil {
			if err := f.Check(v); err != nil {
				return nil, err
			}
		} else if p.Generated && !f.Secret && !f.JSON && len(v) > 1024 {
			return nil, fmt.Errorf("%s is too long", k)
		}
		if f.Multi {
			v = strings.ReplaceAll(v, "\n", `\n`)
		}
		if err := noControl(v); err != nil {
			return nil, fmt.Errorf("%s %v", k, err)
		}
		if f.Choices != nil && !contains(f.Choices, v) {
			return nil, fmt.Errorf("%s must be one of %s", k, strings.Join(f.Choices, ", "))
		}
		out[k] = v
	}
	for _, f := range p.Fields {
		if _, set := out[f.Key]; !set && f.Default != "" {
			out[f.Key] = f.Default
		}
		if _, set := out[f.Key]; f.Required && !set {
			return nil, fmt.Errorf("%s is required", f.Key)
		}
	}
	switch s.Type {
	case "sftp":
		if out["pass"] == "" && out["key_pem"] == "" {
			return nil, errors.New("give the password or the private key of the SFTP user")
		}
	case "s3":
		if out["provider"] != "AWS" && out["endpoint"] == "" {
			return nil, errors.New("the endpoint is required for this provider")
		}
	}
	if p.Bucket {
		if !bucketRe.MatchString(s.Bucket) {
			return nil, errors.New("the bucket name is not valid")
		}
	} else if s.Bucket != "" {
		return nil, errors.New("this storage has no bucket")
	}
	if s.Folder != "" {
		if !folderRe.MatchString(s.Folder) {
			return nil, errors.New("the folder may hold letters, digits, dot, dash and underscore, with / between folders")
		}
		for _, part := range strings.Split(s.Folder, "/") {
			if part == "." || part == ".." {
				return nil, errors.New("the folder may not hold . or ..")
			}
		}
	}
	for k, v := range p.Fixed {
		out[k] = v
	}
	return out, nil
}

// Allowed lists every key a remote of this type may hold in rclone.conf: the fields, the fixed settings and "type".
func (p Provider) Allowed() map[string]bool {
	out := map[string]bool{"type": true}
	for _, f := range p.Fields {
		out[f.Key] = true
	}
	for k := range p.Fixed {
		out[k] = true
	}
	return out
}

// SecretKeys lists the fields rclone keeps obscured in its config file. key_pem is read as it is.
func (p Provider) SecretKeys() map[string]bool {
	out := map[string]bool{}
	for _, f := range p.Fields {
		if f.Secret && f.Key != "key_pem" {
			out[f.Key] = true
		}
	}
	return out
}

// Shown keeps the values a form may show again: everything but the secrets.
func (p Provider) Shown(values map[string]string) map[string]string {
	out := map[string]string{}
	for _, f := range p.Fields {
		if v, ok := values[f.Key]; ok && !f.Secret && v != "" {
			out[f.Key] = v
		}
	}
	return out
}

// Dangerous reports whether the name of an rclone option says it runs a command or reads a file of this machine. A test of
// internal/remote runs it over every option of every backend mpn imports, so a new option of a later rclone is caught there.
func Dangerous(key string) bool {
	k := strings.ToLower(key)
	for _, w := range []string{"command", "cmd", "exec", "_file", "file_", "ssh", "agent", "socket", "proxy", "env_auth", "path_override",
		"_path", "use_az", "kerberos", "ccache", "known_hosts", "template", "service_principal"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

// RootName is the folder that holds everything mpn uploads.
const RootName = "mpn-backups"

// Root is the path in the storage that holds the folder of every site: [<bucket>/][<folder>/]mpn-backups.
func Root(bucket, folder string) string {
	var parts []string
	if bucket != "" {
		parts = append(parts, bucket)
	}
	if folder != "" {
		parts = append(parts, strings.Trim(folder, "/"))
	}
	return strings.Join(append(parts, RootName), "/")
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
