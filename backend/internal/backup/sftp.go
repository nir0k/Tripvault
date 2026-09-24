package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The keys an SFTP destination reads out of a configuration's parameters.
//
// None of them is a secret: the password and the private key are typed into the
// form and kept sealed. A parameter naming an environment variable to read a
// credential out of is deliberately absent - it would let whoever can edit a
// configuration point it at a machine of their choosing and hand it any variable
// the service holds, the signing secret included.
const (
	sftpHost    = "host"
	sftpPort    = "port"
	sftpPath    = "path"
	sftpUser    = "user"
	sftpHostKey = "host_key"
)

// The credentials an SFTP destination can be given. They never sit in the
// parameters: each is sealed with the instance's secrets key and kept in the
// configuration's sealed secrets, and is opened only to make the connection.
const (
	SFTPSecretPassword      = "password"
	SFTPSecretPrivateKey    = "private_key"
	SFTPSecretKeyPassphrase = "key_passphrase"
)

// sftpSecretNames is the set the constants above declare.
var sftpSecretNames = map[string]bool{
	SFTPSecretPassword:      true,
	SFTPSecretPrivateKey:    true,
	SFTPSecretKeyPassphrase: true,
}

// IsSFTPSecret - reports whether a name is a credential an SFTP destination takes.
//
// Arguments:
//   - name: the credential's name as the API received it.
//
// Returns:
//   - true for password, private_key and key_passphrase.
func IsSFTPSecret(name string) bool {
	return sftpSecretNames[name]
}

// defaultSFTPPort is what every SSH server listens on unless told otherwise.
const defaultSFTPPort = 22

// defaultSFTPPath is where archives go when no directory is named: relative, so
// it lands in the account's own home directory, which is the one place an
// account can be assumed to be able to write.
const defaultSFTPPath = "tripvault-backups"

// sftpTimeout bounds opening the connection. A backup that hangs on a server
// that has gone away would hold the scheduler until the pass times out.
const sftpTimeout = 30 * time.Second

// ValidateSFTPParams - checks that a configuration describes a server that can
// actually be reached, without reaching it.
//
// It runs when a configuration is saved, so that a missing host or an unreadable
// host key is refused there rather than failing every night afterwards with
// nobody watching. Credentials are checked separately, by
// ValidateSFTPCredentials, because they may be sealed rather than in params.
//
// Arguments:
//   - params: the destination parameters.
//
// Returns:
//   - a *domain.ValidationError describing what is missing or unreadable.
func ValidateSFTPParams(params map[string]string) error {
	for _, required := range []string{sftpHost, sftpUser} {
		if strings.TrimSpace(params[required]) == "" {
			return domain.NewValidationError("destination_params."+required, "required",
				"an SFTP destination needs a "+required)
		}
	}

	if _, err := sftpPortOf(params); err != nil {
		return err
	}

	// The host key may be left out, in which case the first connection learns it
	// and every later one is held to it. One that is given has to be readable: a
	// paste that went wrong should be refused here, not treated as "no key" and
	// replaced by whatever answers.
	if fingerprint := strings.TrimSpace(params[sftpHostKey]); fingerprint != "" {
		if _, err := parseHostKey(fingerprint); err != nil {
			return domain.NewValidationError("destination_params."+sftpHostKey, "unreadable",
				"the host key could not be read: "+err.Error())
		}
	}
	return nil
}

// ValidateSFTPCredentials - checks that an SFTP destination has a way to sign in.
//
// A password or a private key is enough. A private key that has just been typed
// in is also read, so a paste that lost a line is refused while the form is
// still open.
//
// Arguments:
//   - stored: the names of the credentials the configuration will hold sealed.
//   - given: the credentials that arrived with this request, in the clear.
//
// Returns:
//   - a *domain.ValidationError when there is no way to sign in or the given key
//     cannot be read.
func ValidateSFTPCredentials(stored []string, given map[string]string) error {
	holds := func(name string) bool {
		for _, candidate := range stored {
			if candidate == name {
				return true
			}
		}
		return false
	}

	if !holds(SFTPSecretPassword) && !holds(SFTPSecretPrivateKey) {
		return domain.NewValidationError("secrets", "required",
			"an SFTP destination needs a password or a private key")
	}

	pem := given[SFTPSecretPrivateKey]
	if strings.TrimSpace(pem) == "" {
		return nil
	}
	// A new key is read with the passphrase that came with it, never with one
	// kept from before: a passphrase belongs to the key it was entered for.
	if _, err := parseSigner(pem, given[SFTPSecretKeyPassphrase]); err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return domain.NewValidationError("secrets.key_passphrase", "required",
				"the private key is protected by a passphrase; enter it too")
		}
		return domain.NewValidationError("secrets.private_key", "unreadable",
			"the private key could not be read: "+err.Error())
	}
	return nil
}

// sftpPortOf reads the port, defaulting to 22.
func sftpPortOf(params map[string]string) (int, error) {
	raw := strings.TrimSpace(params[sftpPort])
	if raw == "" {
		return defaultSFTPPort, nil
	}

	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, domain.NewValidationError("destination_params."+sftpPort, "invalid_port",
			"must be a port number between 1 and 65535")
	}
	return port, nil
}

// parseHostKey reads a host key in the form ssh-keyscan prints.
//
// Both shapes are accepted: the whole line with the host in front, as the file
// keeps it, and the key on its own. Somebody pasting from a known_hosts file
// should not have to know which half this wanted.
func parseHostKey(value string) (ssh.PublicKey, error) {
	fields := strings.Fields(value)
	if len(fields) >= 3 {
		fields = fields[1:]
	}
	if len(fields) < 2 {
		return nil, errors.New("expected a line like \"ssh-ed25519 AAAAC3...\"")
	}

	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(fields[0] + " " + fields[1]))
	if err != nil {
		return nil, err
	}
	return key, nil
}

// SFTPDestination writes archives to a directory on another machine.
type SFTPDestination struct {
	client *sftp.Client
	ssh    *ssh.Client
	root   string
	// learnedHostKey is the key the server presented on a connection made with
	// no key configured, in authorized_keys form; empty when a key was pinned.
	learnedHostKey string
}

// DialSFTP - opens a connection to the server a configuration names.
//
// The connection is made when a backup runs rather than held open: a schedule
// that fires nightly would otherwise be reaching for a connection that died
// hours ago.
//
// With a host key configured, the server must present exactly that key. With
// none, the key it presents is accepted and reported through LearnedHostKey, and
// the caller is expected to pin it: every connection after the first is then
// held to it, so a server that changes identity later is refused.
//
// Arguments:
//   - params: the destination parameters.
//   - secrets: the credentials the configuration holds sealed, already opened.
//
// Returns:
//   - the destination, which the caller must close.
//   - an error if the parameters are unusable or the server cannot be reached.
func DialSFTP(params map[string]string, secrets map[string]string) (*SFTPDestination, error) {
	if err := ValidateSFTPParams(params); err != nil {
		return nil, err
	}

	port, err := sftpPortOf(params)
	if err != nil {
		return nil, err
	}

	methods, err := sftpAuthMethods(secrets)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User:    strings.TrimSpace(params[sftpUser]),
		Auth:    methods,
		Timeout: sftpTimeout,
	}

	var learned ssh.PublicKey
	if pinned := strings.TrimSpace(params[sftpHostKey]); pinned != "" {
		expected, err := parseHostKey(pinned)
		if err != nil {
			return nil, err
		}
		// The key is pinned to the one the configuration names, and anything
		// else is refused.
		config.HostKeyCallback = ssh.FixedHostKey(expected)
		// A real server holds several host keys - RSA and ed25519 as a rule -
		// and the client picks which to ask for from its own preference list,
		// where RSA comes first. Pinning an ed25519 key and letting the client
		// choose would compare it against the RSA key and refuse a server that
		// is exactly the one configured. So the client asks for the pinned kind.
		config.HostKeyAlgorithms = hostKeyAlgorithms(expected)
	} else {
		// Trust on first use: this connection is the one that decides which
		// server the configuration means, and the caller records the answer.
		config.HostKeyCallback = func(_ string, _ net.Addr, key ssh.PublicKey) error {
			learned = key
			return nil
		}
	}

	address := net.JoinHostPort(strings.TrimSpace(params[sftpHost]), strconv.Itoa(port))
	connection, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", address, err)
	}

	client, err := sftp.NewClient(connection)
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("open sftp on %s: %w", address, err)
	}

	root := strings.TrimSpace(params[sftpPath])
	if root == "" {
		root = defaultSFTPPath
	}
	if err := client.MkdirAll(root); err != nil {
		_ = client.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("create %s on the server: %w", root, err)
	}

	destination := &SFTPDestination{client: client, ssh: connection, root: root}
	if learned != nil {
		destination.learnedHostKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(learned)))
	}
	return destination, nil
}

// LearnedHostKey - returns the host key this connection accepted on first use.
//
// Returns:
//   - the key in authorized_keys form, or empty when the configuration pinned one.
func (d *SFTPDestination) LearnedHostKey() string {
	return d.learnedHostKey
}

// hostKeyAlgorithms names the algorithms a pinned host key can be presented
// under. An RSA key has three - its SHA-2 signatures and the legacy SHA-1 one -
// and every other kind is its own single algorithm.
func hostKeyAlgorithms(key ssh.PublicKey) []string {
	if key.Type() == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{key.Type()}
}

// sftpAuthMethods builds the ways to authenticate from the configuration's
// stored credentials. The key is offered before the password when both exist.
func sftpAuthMethods(secrets map[string]string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if strings.TrimSpace(secrets[SFTPSecretPrivateKey]) != "" {
		signer, err := parseSigner(secrets[SFTPSecretPrivateKey], secrets[SFTPSecretKeyPassphrase])
		if err != nil {
			return nil, fmt.Errorf("read the stored private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if secrets[SFTPSecretPassword] != "" {
		methods = append(methods, ssh.Password(secrets[SFTPSecretPassword]))
	}

	if len(methods) == 0 {
		return nil, errors.New("no way to authenticate was configured")
	}
	return methods, nil
}

// parseSigner reads a private key in PEM form, with its passphrase when it has one.
func parseSigner(pem, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase([]byte(pem), []byte(passphrase))
	}
	return ssh.ParsePrivateKey([]byte(pem))
}

// Close - releases the connection.
//
// Returns:
//   - the first error either half reported, or nil.
func (d *SFTPDestination) Close() error {
	clientErr := d.client.Close()
	sshErr := d.ssh.Close()
	if clientErr != nil {
		return clientErr
	}
	return sshErr
}

// resolve turns an archive name into a remote path, refusing anything that is
// not a plain filename.
//
// The names this service generates are always plain, but a name also arrives
// from a database row, and a row is not a place to take a path from on trust.
func (d *SFTPDestination) resolve(name string) (string, error) {
	if name == "" || name != path.Base(name) || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("%q is not a valid archive name", name)
	}
	return path.Join(d.root, name), nil
}

// Store - writes an archive to the server.
//
// It is written under a temporary name and renamed into place, so a transfer cut
// off half way leaves no partial file that a later restore might take for a good
// one. The rename refuses to replace an existing archive, because the one thing
// this must never do is destroy a good backup.
//
// Arguments:
//   - ctx: cancelling it abandons the transfer.
//   - name: the archive's filename.
//   - r: the archive's bytes.
//
// Returns:
//   - the number of bytes written.
//   - an error if the transfer fails.
func (d *SFTPDestination) Store(ctx context.Context, name string, r io.Reader) (int64, error) {
	target, err := d.resolve(name)
	if err != nil {
		return 0, err
	}

	if _, err := d.client.Stat(target); err == nil {
		return 0, fmt.Errorf("an archive named %s is already there", name)
	}

	partial := target + ".partial"
	file, err := d.client.Create(partial)
	if err != nil {
		return 0, fmt.Errorf("create %s on the server: %w", partial, err)
	}

	written, err := io.Copy(file, readerWithContext{ctx: ctx, r: r})
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = d.client.Remove(partial)
		return 0, fmt.Errorf("write %s to the server: %w", name, err)
	}

	if err := d.client.Rename(partial, target); err != nil {
		_ = d.client.Remove(partial)
		return 0, fmt.Errorf("put %s in place on the server: %w", name, err)
	}
	return written, nil
}

// Open - reads an archive back from the server.
//
// Arguments:
//   - ctx: unused by the transfer, present for the interface.
//   - name: the archive's filename.
//
// Returns:
//   - a reader the caller must close.
//   - domain.ErrNotFound when no such archive is held.
func (d *SFTPDestination) Open(_ context.Context, name string) (io.ReadCloser, error) {
	target, err := d.resolve(name)
	if err != nil {
		return nil, err
	}

	file, err := d.client.Open(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open %s on the server: %w", name, err)
	}
	return file, nil
}

// List - returns the archives held on the server, newest first.
//
// Arguments:
//   - ctx: unused by the transfer, present for the interface.
//
// Returns:
//   - the archives, newest first.
//   - an error if the directory cannot be read.
func (d *SFTPDestination) List(_ context.Context) ([]Artifact, error) {
	entries, err := d.client.ReadDir(d.root)
	if err != nil {
		return nil, fmt.Errorf("read %s on the server: %w", d.root, err)
	}

	artifacts := make([]Artifact, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ArchivePrefix) {
			continue
		}
		artifacts = append(artifacts, Artifact{
			Name:      entry.Name(),
			SizeBytes: entry.Size(),
			WrittenAt: entry.ModTime(),
		})
	}

	// By name rather than by modification time, for the same reason the local
	// destination does: the name carries the instant the archive was taken, and
	// it does not move when a file is copied about.
	sort.Slice(artifacts, func(a, b int) bool {
		return artifacts[a].Name > artifacts[b].Name
	})
	return artifacts, nil
}

// Remove - deletes one archive from the server.
//
// Arguments:
//   - ctx: unused by the transfer, present for the interface.
//   - name: the archive's filename.
//
// Returns:
//   - an error if the file exists and cannot be removed.
func (d *SFTPDestination) Remove(_ context.Context, name string) error {
	target, err := d.resolve(name)
	if err != nil {
		return err
	}
	if err := d.client.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s from the server: %w", name, err)
	}
	return nil
}

// readerWithContext stops a transfer when the context is cancelled.
//
// The SFTP client has no context of its own, so this is where a shutdown or a
// timed-out pass actually takes effect: the next read fails and the copy unwinds.
type readerWithContext struct {
	ctx context.Context
	r   io.Reader
}

// Read passes the read through unless the context has been cancelled.
func (r readerWithContext) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
