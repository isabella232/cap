package oidc

import (
	"fmt"
	"time"
)

// State basically represents one OIDC authentication flow for a user. It
// contains the data needed to uniquely represent that one-time flow across the
// multiple interactions needed to complete the OIDC flow the user is
// attempting.
//
// ID() is passed throughout the OIDC interactions to uniquely identify the
// flow's state. The ID() and Nonce() cannot be equal, and will be used during
// the OIDC flow to prevent CSRF and replay attacks (see the oidc spec for
// specifics).
//
// Audiences, Scopes, and RedirectURL are optional overrides of configured
// provider defaults for specific authentication attempts
type State interface {
	// ID is a unique identifier and an opaque value used to maintain state
	// between the oidc request and the callback. ID cannot equal the Nonce.
	// See https://openid.net/specs/openid-connect-core-1_0.html#AuthRequest.
	ID() string

	// Nonce is a unique nonce and a string value used to associate a Client
	// session with an ID Token, and to mitigate replay attacks. Nonce cannot
	// equal the ID.
	// See https://openid.net/specs/openid-connect-core-1_0.html#AuthRequest
	// and https://openid.net/specs/openid-connect-core-1_0.html#NonceNotes.
	Nonce() string

	// IsExpired returns true if the state has expired. Implementations should
	// support a time skew (perhaps StateExpirySkew) when checking expiration.
	IsExpired() bool

	// Audiences is an specific authentication attempt's list of optional
	// case-sensitive strings to use when verifying an id_token's "aud" claim
	// (which is also a list). If provided, the audiences of an id_token must
	// match one of the configured audiences.  If a State does not have
	// audiences, then the configured list of default audiences will be used.
	Audiences() []string

	// Scopes is a specific authentication attempt's list of optional
	// scopes to request of the provider. The required "oidc" scope is requested
	// by default, and does not need to be part of this optional list. If a
	// State does not have Scopes, then the configured list of default
	// requested scopes will be used.
	Scopes() []string

	// RedirectURL is a specific authentication attempt's redirect URL, where
	// providers will direct responses to authentication requests. Of a State
	// does not have a RedirectURL, then the configured default redirect URL
	// will be used.
	RedirectURL() string
}

// St represents the oidc state used for oidc flows and implements the State interface.
type St struct {
	//	id is a unique identifier and an opaque value used to maintain state
	//	between the oidc request and the callback.
	id string

	// nonce is a unique nonce and suitable for use as an oidc nonce.
	nonce string

	// Expiration is the expiration time for the State.
	expiration time.Time

	// redirectURL is a specific authentication attempt's redirect URL, where
	// providers will direct responses to authentication requests. Of a State
	// does not have a RedirectURL, then the configured default redirect URL
	// will be used.
	redirectURL string

	// scopes is a specific authentication attempt's list of optional
	// scopes to request of the provider. The required "oidc" scope is requested
	// by default, and does not need to be part of this optional list. If a
	// State does not have Scopes, then the configured list of default
	// requested scopes will be used.
	scopes []string

	// audiences is an specific authentication attempt's list of optional
	// case-sensitive strings to use when verifying an id_token's "aud" claim
	// (which is also a list). If provided, the audiences of an id_token must
	// match one of the configured audiences.  If a State does not have
	// audiences, then the configured list of default audiences will be used.
	audiences []string

	// nowFunc is an optional function that returns the current time
	nowFunc func() time.Time
}

// ensure that St implements the State interface.
var _ State = (*St)(nil)

// NewState creates a new State (*St).
//  Supports the options:
//   * WithNow: (with a default to time.Now).
//   * WithRedirectURL
//   * WithAudiences
//   * WithScopes
func NewState(expireIn time.Duration, opt ...Option) (*St, error) {
	const op = "oidc.NewState"
	opts := getStOpts(opt...)

	nonce, err := NewID(WithPrefix("n"))
	if err != nil {
		return nil, fmt.Errorf("%s: unable to generate a state's nonce: %w", op, err)
	}

	id, err := NewID(WithPrefix("st"))
	if err != nil {
		return nil, fmt.Errorf("%s: unable to generate a state's id: %w", op, err)
	}
	if expireIn == 0 || expireIn < 0 {
		return nil, fmt.Errorf("%s: expireIn not greater than zero: %w", op, ErrInvalidParameter)
	}
	s := &St{
		id:          id,
		nonce:       nonce,
		nowFunc:     opts.withNowFunc,
		redirectURL: opts.withRedirectURL,
		audiences:   opts.withAudiences,
		scopes:      opts.withScopes,
	}
	s.expiration = s.now().Add(expireIn)
	return s, nil
}

func (s *St) ID() string          { return s.id }    // ID implements the State.ID() interface function.
func (s *St) Nonce() string       { return s.nonce } // Nonce implements the State.Nonce() interface function.
func (s *St) Audiences() []string { return s.audiences }
func (s *St) Scopes() []string    { return s.scopes }
func (s *St) RedirectURL() string { return s.redirectURL }

// StateExpirySkew defines a time skew when checking a State's expiration.
const StateExpirySkew = 1 * time.Second

// IsExpired returns true if the state has expired.
func (s *St) IsExpired() bool {
	return s.expiration.Before(time.Now().Add(StateExpirySkew))
}

// now returns the current time using the optional timeFn
func (s *St) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now() // fallback to this default
}

// stOptions is the set of available options for St functions
type stOptions struct {
	withNowFunc     func() time.Time
	withScopes      []string
	withRedirectURL string
	withAudiences   []string
}

// stDefaults is a handy way to get the defaults at runtime and during unit
// tests.
func stDefaults() stOptions {
	return stOptions{}
}

// getStateOpts gets the state defaults and applies the opt overrides passed in
func getStOpts(opt ...Option) stOptions {
	opts := stDefaults()
	ApplyOpts(&opts, opt...)
	return opts
}

// WithRedirectURL is the URL where the provider will send responses to
// authentication requests. Valid for: St
func WithRedirectURL(redirect string) Option {
	return func(o interface{}) {
		if o, ok := o.(*stOptions); ok {
			o.withRedirectURL = redirect
		}
	}
}
