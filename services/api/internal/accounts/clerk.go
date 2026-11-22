package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwks"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/clerk/clerk-sdk-go/v2/user"
)

// ErrClerkVerification marks a Clerk session token or user that cannot be trusted.
var ErrClerkVerification = errors.New("clerk identity rejected")

// ClerkIdentity holds only claims the API has verified itself: the session token
// signature, issuer and authorized party, plus the Clerk user's primary email as
// returned by the Clerk Backend API.
type ClerkIdentity struct {
	UserID        string
	Email         string
	EmailVerified bool
	Name          string
}

// ClerkVerifier turns a Clerk session token into a verified identity. Tests inject a
// fake; production uses NewClerkVerifier. A nil verifier keeps SSO switched off.
type ClerkVerifier interface {
	Verify(ctx context.Context, sessionToken string) (ClerkIdentity, error)
}

// ClerkUsers is the part of the Clerk Backend API the verifier needs.
type ClerkUsers interface {
	Get(ctx context.Context, id string) (*clerk.User, error)
}

// ClerkConfig configures the production verifier. Keys come from the environment.
type ClerkConfig struct {
	// SecretKey authenticates Backend API calls (user lookup, JWKS fetch).
	SecretKey string
	// JWTKey is the optional PEM public key that enables networkless verification.
	JWTKey string
	// AuthorizedParties are the web origins allowed in the token's azp claim.
	AuthorizedParties []string
	// Users and Keys replace the live Clerk clients in tests.
	Users ClerkUsers
	Keys  *clerk.JSONWebKey
	Clock clerk.Clock
}

type clerkVerifier struct {
	cfg   ClerkConfig
	jwks  *jwks.Client
	users ClerkUsers
	mu    sync.Mutex
	keys  map[string]cachedKey
}

type cachedKey struct {
	key     *clerk.JSONWebKey
	expires time.Time
}

// NewClerkVerifier validates configuration and builds the production verifier.
func NewClerkVerifier(cfg ClerkConfig) (ClerkVerifier, error) {
	if cfg.Users == nil && !strings.HasPrefix(cfg.SecretKey, "sk_") {
		return nil, errors.New("CLERK_SECRET_KEY must be a Clerk secret key (sk_...)")
	}
	if len(cfg.AuthorizedParties) == 0 {
		return nil, errors.New("CLERK_AUTHORIZED_PARTIES must list at least one web origin")
	}
	v := &clerkVerifier{cfg: cfg, keys: map[string]cachedKey{}}
	clientConfig := &clerk.ClientConfig{BackendConfig: clerk.BackendConfig{Key: clerk.String(cfg.SecretKey)}}
	v.jwks = jwks.NewClient(clientConfig)
	v.users = cfg.Users
	if v.users == nil {
		v.users = user.NewClient(clientConfig)
	}
	if cfg.JWTKey != "" && cfg.Keys == nil {
		key, err := clerk.JSONWebKeyFromPEM(cfg.JWTKey)
		if err != nil {
			return nil, fmt.Errorf("CLERK_JWT_KEY: %w", err)
		}
		v.cfg.Keys = key
	}
	return v, nil
}

func (v *clerkVerifier) key(ctx context.Context, token string) (*clerk.JSONWebKey, error) {
	if v.cfg.Keys != nil {
		return v.cfg.Keys, nil
	}
	decoded, err := jwt.Decode(ctx, &jwt.DecodeParams{Token: token})
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	cached, ok := v.keys[decoded.KeyID]
	v.mu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return cached.key, nil
	}
	key, err := jwt.GetJSONWebKey(ctx, &jwt.GetJSONWebKeyParams{KeyID: decoded.KeyID, JWKSClient: v.jwks})
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	v.keys[decoded.KeyID] = cachedKey{key: key, expires: time.Now().Add(time.Hour)}
	v.mu.Unlock()
	return key, nil
}

func (v *clerkVerifier) Verify(ctx context.Context, token string) (ClerkIdentity, error) {
	if token == "" || len(token) > 8192 {
		return ClerkIdentity{}, ErrClerkVerification
	}
	key, err := v.key(ctx, token)
	if err != nil {
		return ClerkIdentity{}, errors.Join(ErrClerkVerification, err)
	}
	allowed := map[string]bool{}
	for _, party := range v.cfg.AuthorizedParties {
		allowed[party] = true
	}
	claims, err := jwt.Verify(ctx, &jwt.VerifyParams{
		Token:                  token,
		JWK:                    key,
		Clock:                  v.cfg.Clock,
		Leeway:                 5 * time.Second,
		AuthorizedPartyHandler: func(party string) bool { return allowed[party] },
	})
	if err != nil || claims.Subject == "" {
		return ClerkIdentity{}, errors.Join(ErrClerkVerification, err)
	}
	account, err := v.users.Get(ctx, claims.Subject)
	if err != nil {
		return ClerkIdentity{}, fmt.Errorf("clerk user lookup: %w", err)
	}
	if account == nil || account.ID != claims.Subject || account.Banned || account.Locked {
		return ClerkIdentity{}, ErrClerkVerification
	}
	identity := ClerkIdentity{UserID: account.ID}
	if account.FirstName != nil {
		identity.Name = strings.TrimSpace(*account.FirstName)
	}
	if account.PrimaryEmailAddressID != nil {
		for _, address := range account.EmailAddresses {
			if address != nil && address.ID == *account.PrimaryEmailAddressID {
				identity.Email = address.EmailAddress
				identity.EmailVerified = trustedEmailVerification(address.Verification)
			}
		}
	}
	return identity, nil
}

// trustedEmailVerification accepts only proof of mailbox control that Duolify can rely
// on for account linking: Clerk's own email code, or a verified email returned by Google
// or Apple sign-in. Status must be "verified" on the primary address itself; admin-set,
// invitation and any other strategy never count.
func trustedEmailVerification(v *clerk.Verification) bool {
	if v == nil || v.Status != "verified" {
		return false
	}
	switch v.Strategy {
	case "email_code", "from_oauth_google", "from_oauth_apple":
		return true
	}
	return false
}
