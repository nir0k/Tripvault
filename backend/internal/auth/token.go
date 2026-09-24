package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// tokenIssuer identifies this service in the "iss" claim, so a token minted for
// another service can never be mistaken for one of ours.
const tokenIssuer = "tripvault"

// refreshTokenBytes is the entropy of an opaque refresh token.
const refreshTokenBytes = 32

// Claims are the access token's payload: who the user is and which session the
// token belongs to. Roles are deliberately absent - they are read from the
// database on every request, so a withdrawn right does not outlive the change.
type Claims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

// AccessToken is what a validated access token names.
type AccessToken struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

// TokenIssuer mints and validates the tokens that make up a session.
type TokenIssuer struct {
	secret          []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	// now is injected so that expiry can be tested without sleeping.
	now func() time.Time
}

// NewTokenIssuer - creates the token issuer for a signing secret.
//
// Arguments:
//   - secret: key used to sign and verify access tokens. Changing it invalidates
//     every access token already in circulation.
//   - accessTTL: lifetime of an access token.
//   - refreshTTL: lifetime of a refresh token.
//
// Returns:
//   - a ready issuer.
//   - an error if the secret is empty, which would make tokens forgeable.
func NewTokenIssuer(secret string, accessTTL, refreshTTL time.Duration) (*TokenIssuer, error) {
	if secret == "" {
		return nil, errors.New("token signing secret must not be empty")
	}
	return &TokenIssuer{
		secret:          []byte(secret),
		accessTokenTTL:  accessTTL,
		refreshTokenTTL: refreshTTL,
		now:             time.Now,
	}, nil
}

// AccessTokenTTL - reports how long a freshly issued access token stays valid.
//
// Returns:
//   - the configured access token lifetime.
func (t *TokenIssuer) AccessTokenTTL() time.Duration {
	return t.accessTokenTTL
}

// RefreshTokenTTL - reports how long a session lasts after its last refresh.
//
// Returns:
//   - the configured refresh token lifetime.
func (t *TokenIssuer) RefreshTokenTTL() time.Duration {
	return t.refreshTokenTTL
}

// IssueAccessToken - signs a short-lived access token for a session.
//
// Arguments:
//   - userID: the account the token authenticates.
//   - sessionID: the session it belongs to, so revoking the session revokes
//     the token too.
//
// Returns:
//   - the signed token in compact JWT form.
//   - an error if signing fails.
func (t *TokenIssuer) IssueAccessToken(userID, sessionID uuid.UUID) (string, error) {
	issuedAt := t.now()
	claims := Claims{
		SessionID: sessionID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			NotBefore: jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(issuedAt.Add(t.accessTokenTTL)),
			ID:        uuid.NewString(),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

// ParseAccessToken - validates an access token and extracts what it names.
//
// The signing method is pinned to HMAC-SHA256, which blocks re-signing a token
// with "alg": "none" or with an attacker-supplied key.
//
// Arguments:
//   - token: the compact JWT presented by the client.
//
// Returns:
//   - the user and session the token belongs to.
//   - domain.ErrTokenInvalid if the token is malformed, expired, or not signed
//     by this service.
func (t *TokenIssuer) ParseAccessToken(token string) (AccessToken, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{},
		func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return AccessToken{}, fmt.Errorf("%w: %s", domain.ErrTokenInvalid, err)
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok {
		return AccessToken{}, fmt.Errorf("%w: unexpected claims type", domain.ErrTokenInvalid)
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return AccessToken{}, fmt.Errorf("%w: subject is not a user id", domain.ErrTokenInvalid)
	}
	sessionID, err := uuid.Parse(claims.SessionID)
	if err != nil {
		return AccessToken{}, fmt.Errorf("%w: sid is not a session id", domain.ErrTokenInvalid)
	}
	return AccessToken{UserID: userID, SessionID: sessionID}, nil
}

// newOpaqueToken generates size bytes of entropy as a URL-safe string, with its
// lookup hash. base64url keeps the token safe in a URL fragment and a header.
func newOpaqueToken(size int) (string, []byte, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

// hashToken derives the lookup hash of an opaque token.
//
// A plain SHA-256 is deliberate rather than a slow password hash: the token is
// machine-generated entropy, so there is nothing to brute-force, and the lookup
// must stay cheap on every request that carries one.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// NewRefreshToken - generates an opaque refresh token and its lookup hash.
//
// Returns:
//   - the plaintext token, to be handed to the client once.
//   - its hash, the only form the database keeps.
//   - an error if the system's random source is unavailable.
func NewRefreshToken() (string, []byte, error) {
	return newOpaqueToken(refreshTokenBytes)
}

// HashRefreshToken - derives the lookup hash stored for a refresh token.
//
// Arguments:
//   - token: the plaintext refresh token.
//
// Returns:
//   - the hash used as the database key.
func HashRefreshToken(token string) []byte {
	return hashToken(token)
}

// NewShareToken - generates the token of a share link and its lookup hash.
//
// The plaintext is shown to the owner once, in the answer that creates the link,
// and cannot be recovered afterwards: a lost link is replaced, not retrieved.
//
// Returns:
//   - the plaintext token, for the link handed to the owner once.
//   - its hash, the only form the database keeps.
//   - an error if the system's random source is unavailable.
func NewShareToken() (string, []byte, error) {
	return newOpaqueToken(domain.ShareTokenBytes)
}

// HashShareToken - derives the lookup hash stored for a share token.
//
// Arguments:
//   - token: the token read from the X-Share-Token header.
//
// Returns:
//   - the hash used to find the link.
func HashShareToken(token string) []byte {
	return hashToken(token)
}
