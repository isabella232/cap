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
// Audiences and Scopes are optional overrides of configured provider defaults
// for specific authentication attempts
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

	// RedirectURL is a URL where providers will redirect responses to
	// authentication requests.
	RedirectURL() string

	// ImplicitFlow indicates whether or not to use the implicit flow with form
	// post.  It should be noted that if your OIDC provider supports PKCE, then
	// use it over the implicit flow. Getting only an id_token for an implicit
	// flow is the default, but at times it's necessary to also request an
	// access_token, so includeAccessToken allows for those scenarios. It is
	// recommend to not request access_tokens during the implicit flow.  If you
	// need an access_token, then use the authorization code flows and if you
	// can't secure a client secret then use the authorization code flow with
	// PKCE.
	//
	// See: https://openid.net/specs/openid-connect-core-1_0.html#ImplicitFlowAuth
	// See: https://openid.net/specs/oauth-v2-form-post-response-mode-1_0.html
	ImplicitFlow() (useImplicitFlow bool, includeAccessToken bool)

	// PKCEVerifier indicates whether or not to use the authorization code flow
	// with PKCE.  PKCE should be used for any client which cannot secure a
	// client secret (SPA and native apps) or is susceptible to authorization
	// code intercept attacks. When supported by your OIDC provider, PKCE should
	// be used instead of the implicit flow.
	//
	// See: https://tools.ietf.org/html/rfc7636
	PKCEVerifier() CodeVerifier
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

	// redirectURL is a URL where providers will redirect responses to
	// authentication requests.
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

	// withImplicit indicates whether or not to use the implicit flow.  Getting
	// only an id_token for an implicit flow is the default. If an access_token
	// is also required, then withImplicit.includeAccessToken will be true. It
	// is recommend to not request access_tokens during the implicit flow.  If
	// you need an access_token, then use the authorization code flows (with
	// optional PKCE).
	withImplicit *implicitFlow

	// withVerifier indicates whether or not to use the authorization code flow
	// with PKCE.  It suppies the required CodeVerifier for PKCE.
	withVerifier CodeVerifier
}

// ensure that St implements the State interface.
var _ State = (*St)(nil)

// NewState creates a new State (*St).
//  Supports the options:
//   * WithNow
//   * WithAudiences
//   * WithScopes
//   * WithImplicit
//   * WithPKCE
func NewState(expireIn time.Duration, redirectURL string, opt ...Option) (*St, error) {
	const op = "oidc.NewState"
	opts := getStOpts(opt...)
	if redirectURL == "" {
		return nil, fmt.Errorf("%s: redirect URL is empty: %w", op, ErrInvalidParameter)
	}
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
	if opts.withVerifier != nil && opts.withImplicitFlow != nil {
		return nil, fmt.Errorf("%s: requested both implicit flow and authorization code with PKCE: %w", op, ErrInvalidParameter)

	}
	s := &St{
		id:           id,
		nonce:        nonce,
		redirectURL:  redirectURL,
		nowFunc:      opts.withNowFunc,
		audiences:    opts.withAudiences,
		scopes:       opts.withScopes,
		withImplicit: opts.withImplicitFlow,
		withVerifier: opts.withVerifier,
	}
	s.expiration = s.now().Add(expireIn)
	return s, nil
}

func (s *St) ID() string                 { return s.id }           // ID implements the State.ID() interface function.
func (s *St) Nonce() string              { return s.nonce }        // Nonce implements the State.Nonce() interface function.
func (s *St) Audiences() []string        { return s.audiences }    // Audiences implements the State.Audiences() interface function.
func (s *St) Scopes() []string           { return s.scopes }       // Scopes implements the State.Scopes() interface function.
func (s *St) RedirectURL() string        { return s.redirectURL }  // RedirectURL implements the State.RedirectURL() interface function.
func (s *St) PKCEVerifier() CodeVerifier { return s.withVerifier } // CodeVerifier implements the State.CodeVerifier() interface function.

// ImplicitFlow indicates whether or not to use the implicit flow.  Getting
// only an id_token for an implicit flow should be the default, but at times
// it's necessary to also request an access_token, so includeAccessToken
// allows for those scenarios. It is recommend to not request access_tokens
// during the implicit flow.  If you need an access_token, then use the
// authorization code flows.
func (s *St) ImplicitFlow() (bool, bool) {
	if s.withImplicit == nil {
		return false, false
	}
	switch {
	case s.withImplicit.withAccessToken:
		return true, true
	default:
		return true, false
	}
}

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

type implicitFlow struct {
	withAccessToken bool
}

// stOptions is the set of available options for St functions
type stOptions struct {
	withNowFunc      func() time.Time
	withScopes       []string
	withAudiences    []string
	withImplicitFlow *implicitFlow
	withVerifier     CodeVerifier
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

// ImplicitFlow indicates whether or not to use the implicit flow with form
// post.  It should be noted that if your OIDC provider supports PKCE, then
// use it over the implicit flow. Getting only an id_token for an implicit
// flow is the default, but at times it's necessary to also request an
// access_token, so includeAccessToken allows for those scenarios. It is
// recommend to not request access_tokens during the implicit flow.  If you
// need an access_token, then use the authorization code flows and if you
// can't secure a client secret then use the authorization code flow with
// PKCE.
//
// See: https://openid.net/specs/openid-connect-core-1_0.html#ImplicitFlowAuth
// See: https://openid.net/specs/oauth-v2-form-post-response-mode-1_0.html

// WithImplicitFlow provides an option to use an OIDC implicit flow with form
// post. It should be noted that if your OIDC provider supports PKCE, then use
// it over the implicit flow.  Getting only an id_token is the default, and
// optionally passing a true bool will request an access_token as well during
// the flow.  You cannot use WithImplicit and WithPKCE together.  It is
// recommend to not request access_tokens during the implicit flow.  If you need
// an access_token, then use the authorization code flows. Option is valid for:
// St
func WithImplicitFlow(args ...interface{}) Option {
	withoutAccessToken := false
	for _, arg := range args {
		switch arg := arg.(type) {
		case bool:
			if arg {
				withoutAccessToken = true
			}
		}
	}
	return func(o interface{}) {
		if o, ok := o.(*stOptions); ok {
			o.withImplicitFlow = &implicitFlow{
				withAccessToken: withoutAccessToken,
			}
		}
	}
}

// WithPKCE provides an option to use a CodeVerifier with the authorization
// code flow with PKCE.  You cannot use WithImplicit and WithPKCE together.
// Option is valid for: St
// See: https://tools.ietf.org/html/rfc7636
func WithPKCE(v CodeVerifier) Option {
	return func(o interface{}) {
		if o, ok := o.(*stOptions); ok {
			o.withVerifier = v
		}
	}
}
