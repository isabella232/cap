package oidc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"golang.org/x/text/language"
)

// TestNewProvider does not repeat all the Config unit tests.  It just focuses
// on the additional tests that are unique to creating a new provider.
func TestNewProvider(t *testing.T) {
	t.Parallel()
	tp := StartTestProvider(t)
	clientID := "test-client-id"
	clientSecret := "test-client-secret"
	redirect := "https://test-redirect"
	tests := []struct {
		name      string
		config    *Config
		wantErr   bool
		wantIsErr error
	}{
		{
			name:   "valid",
			config: testNewConfig(t, clientID, clientSecret, redirect, tp),
		},
		{
			name:      "nil-config",
			config:    nil,
			wantErr:   true,
			wantIsErr: ErrNilParameter,
		},
		{
			name: "invalid-config",
			config: func() *Config {
				c := testNewConfig(t, clientID, clientSecret, redirect, tp)
				c.Issuer = ""
				return c
			}(),
			wantErr:   true,
			wantIsErr: ErrInvalidParameter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			got, err := NewProvider(tt.config)
			if tt.wantErr {
				require.Error(err)
				assert.Truef(errors.Is(err, tt.wantIsErr), "wanted \"%s\" but got \"%s\"", tt.wantIsErr, err)
				return
			}
			require.NoError(err)
			assert.NotNil(got.config)
			assert.NotNil(got.provider)
			assert.NotNil(got.client)
			assert.NotNil(got.backgroundCtx)
			assert.NotNil(got.backgroundCtxCancel)
		})
	}
}

func TestProvider_Done(t *testing.T) {
	t.Parallel()
	tp := StartTestProvider(t)
	p := testNewProvider(t, "client-id", "client-secret", "redirect", tp)

	tests := []struct {
		name                string
		provider            *oidc.Provider
		client              *http.Client
		backgroundCtx       context.Context
		backgroundCtxCancel context.CancelFunc
	}{
		{
			name:                "all-valid",
			provider:            p.provider,
			client:              p.client,
			backgroundCtx:       p.backgroundCtx,
			backgroundCtxCancel: p.backgroundCtxCancel,
		},
		{
			name:                "nil-provider",
			provider:            nil,
			client:              p.client,
			backgroundCtx:       p.backgroundCtx,
			backgroundCtxCancel: p.backgroundCtxCancel,
		},
		{
			name:                "nil-client",
			provider:            p.provider,
			client:              nil,
			backgroundCtx:       p.backgroundCtx,
			backgroundCtxCancel: p.backgroundCtxCancel,
		},
		{
			name:                "nil-backgroundCtx",
			provider:            p.provider,
			client:              p.client,
			backgroundCtx:       p.backgroundCtx,
			backgroundCtxCancel: nil,
		},
		{
			name:                "nil-backgroundCtxCancel",
			provider:            p.provider,
			client:              p.client,
			backgroundCtx:       p.backgroundCtx,
			backgroundCtxCancel: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{
				provider:            tt.provider,
				client:              tt.client,
				backgroundCtx:       tt.backgroundCtx,
				backgroundCtxCancel: tt.backgroundCtxCancel,
			}
			p.Done()
		})
	}
	t.Run("nil-provider", func(t *testing.T) {
		var p *Provider
		p.Done()
	})
}

func TestProvider_AuthURL(t *testing.T) {
	ctx := context.Background()
	clientID := "test-client-id"
	clientSecret := "test-client-secret"
	redirect := "https://test-redirect"
	redirectEncoded := "https%3A%2F%2Ftest-redirect"
	tp := StartTestProvider(t)
	p := testNewProvider(t, clientID, clientSecret, redirect, tp)
	validState, err := NewState(1*time.Second, redirect)
	require.NoError(t, err)

	const reqClaims = `
	{
		"id_token":
		 {
		  "auth_time": {"essential": true},
		  "acr": {"values": ["urn:mace:incommon:iap:silver"] }
		 }
	   }
	   `
	allOptsState, err := NewState(
		1*time.Minute,
		redirect,
		WithAudiences("state-override"),
		WithScopes("email", "profile"),
		WithDisplay(WAP),
		WithPrompts(Login, Consent, SelectAccount),
		WithUILocales(language.AmericanEnglish, language.Spanish),
		WithRequestClaims([]byte(reqClaims)),
		WithACRValues("phr", "phrh"),
	)
	require.NoError(t, err)

	verifier, err := NewCodeVerifier()
	require.NoError(t, err)
	stWithPKCE, err := NewState(
		1*time.Minute,
		redirect,
		WithPKCE(verifier),
	)
	require.NoError(t, err)

	stWithImplicitNoAccessToken, err := NewState(
		1*time.Minute,
		redirect,
		WithImplicitFlow(),
	)
	require.NoError(t, err)

	stWithImplicitWithAccessToken, err := NewState(
		1*time.Minute,
		redirect,
		WithImplicitFlow(true),
	)
	require.NoError(t, err)

	type args struct {
		ctx context.Context
		s   State
	}
	tests := []struct {
		name      string
		p         *Provider
		args      args
		wantURL   string
		wantErr   bool
		wantIsErr error
	}{
		{
			name: "valid-using-default-auth-flow",
			p:    p,
			args: args{
				ctx: ctx,
				s:   validState,
			},
			wantURL: func() string {
				return fmt.Sprintf(
					"%s/authorize?client_id=%s&nonce=%s&redirect_uri=%s&response_type=code&scope=openid&state=%s",
					tp.Addr(),
					clientID,
					validState.Nonce(),
					redirectEncoded,
					validState.ID(),
				)
			}(),
		},
		{
			name: "valid-using-PKCE",
			p:    p,
			args: args{
				ctx: ctx,
				s:   stWithPKCE,
			},
			wantURL: func() string {
				return fmt.Sprintf(
					"%s/authorize?client_id=%s&code_challenge=%s&code_challenge_method=%s&nonce=%s&redirect_uri=%s&response_type=code&scope=openid&state=%s",
					tp.Addr(),
					clientID,
					stWithPKCE.PKCEVerifier().Challenge(),
					stWithPKCE.PKCEVerifier().Method(),
					stWithPKCE.Nonce(),
					redirectEncoded,
					stWithPKCE.ID(),
				)
			}(),
		},
		{
			name: "valid-using-implicit-flow",
			p:    p,
			args: args{
				ctx: ctx,
				s:   stWithImplicitNoAccessToken,
			},
			wantURL: func() string {
				return fmt.Sprintf(
					"%s/authorize?client_id=%s&nonce=%s&redirect_uri=%s&response_mode=form_post&response_type=id_token&scope=openid&state=%s",
					tp.Addr(),
					clientID,
					stWithImplicitNoAccessToken.Nonce(),
					redirectEncoded,
					stWithImplicitNoAccessToken.ID(),
				)
			}(),
		},
		{
			name: "valid-with-all-options-state-except-implicit",
			p:    p,
			args: args{
				ctx: ctx,
				s:   allOptsState,
			},
			wantURL: func() string {
				return fmt.Sprintf(
					"%s/authorize?acr_values=%s&claims=%s&client_id=%s&display=%s&nonce=%s&prompt=%s&redirect_uri=%s&response_type=code&scope=openid+email+profile&state=%s&ui_locales=%s",
					tp.Addr(),
					"phr+phrh", // s.ACRValues() encoded
					// s.RequestClaims() encoded
					`%0A%09%7B%0A%09%09%22id_token%22%3A%0A%09%09+%7B%0A%09%09++%22auth_time%22%3A+%7B%22essential%22%3A+true%7D%2C%0A%09%09++%22acr%22%3A+%7B%22values%22%3A+%5B%22urn%3Amace%3Aincommon%3Aiap%3Asilver%22%5D+%7D%0A%09%09+%7D%0A%09+++%7D%0A%09+++`,
					clientID,
					"wap", // s.Display()
					allOptsState.Nonce(),
					"login+consent+select_account", // s.Prompts() encoded
					redirectEncoded,
					allOptsState.ID(),
					"en-US+es", // s.UILocales() encoded
				)
			}(),
		},
		{
			name: "valid-using-implicit-flow-no-access-token",
			p:    p,
			args: args{
				ctx: ctx,
				s:   stWithImplicitWithAccessToken,
			},
			wantURL: func() string {
				return fmt.Sprintf(
					"%s/authorize?client_id=%s&nonce=%s&redirect_uri=%s&response_mode=form_post&response_type=id_token+token&scope=openid&state=%s",
					tp.Addr(),
					clientID,
					stWithImplicitWithAccessToken.Nonce(),
					redirectEncoded,
					stWithImplicitWithAccessToken.ID(),
				)
			}(),
		},
		{
			name: "implicit-and-PKCE",
			p:    p,
			args: args{
				ctx: ctx,
				s: &St{
					id:           "s_1234567890",
					nonce:        "s_abcdefghigcklmnop",
					expiration:   time.Now().Add(1 * time.Minute),
					redirectURL:  "http://locahost",
					withImplicit: &implicitFlow{},
					withVerifier: verifier,
				},
			},
			wantErr:   true,
			wantIsErr: ErrInvalidParameter,
		},
		{
			name: "empty-redirectURL",
			p:    p,
			args: args{
				ctx: ctx,
				s: &St{
					id:         "s_1234567890",
					nonce:      "s_abcdefghigcklmnop",
					expiration: time.Now().Add(1 * time.Minute),
				},
			},
			wantErr:   true,
			wantIsErr: ErrInvalidParameter,
		},
		{
			name: "bad-redirectURL",
			p:    p,
			args: args{
				ctx: ctx,
				s: &St{
					id:          "s_1234567890",
					nonce:       "s_abcdefghigcklmnop",
					expiration:  time.Now().Add(1 * time.Minute),
					redirectURL: "%%%%%%%%%%%",
				},
			},
			wantErr:   true,
			wantIsErr: ErrInvalidParameter,
		},
		{
			name: "empty-state-nonce",
			p:    p,
			args: args{
				ctx: ctx,
				s: &St{
					id: "s_1234567890",
				},
			},
			wantErr:   true,
			wantIsErr: ErrInvalidParameter,
		},
		{
			name: "empty-state-id",
			p:    p,
			args: args{
				ctx: ctx,
				s: &St{
					nonce: "s_1234567890",
				},
			},
			wantErr:   true,
			wantIsErr: ErrInvalidParameter,
		},
		{
			name: "equal-state-id-and-nonce",
			p:    p,
			args: args{
				ctx: ctx,
				s: &St{
					id:    "s_1234567890",
					nonce: "s_1234567890",
				},
			},
			wantErr:   true,
			wantIsErr: ErrInvalidParameter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			gotURL, err := tt.p.AuthURL(tt.args.ctx, tt.args.s)
			if tt.wantErr {
				require.Error(err)
				assert.Truef(errors.Is(err, tt.wantIsErr), "wanted \"%s\" but got \"%s\"", tt.wantIsErr, err)
				return
			}
			require.NoError(err)
			require.Equalf(tt.wantURL, gotURL, "Provider.AuthURL() = %v, want %v", gotURL, tt.wantURL)
		})
	}
}

func TestProvider_Exchange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clientID := "test-client-id"
	clientSecret := "test-client-secret"
	redirect := "https://test-redirect"

	tp := StartTestProvider(t)
	tp.SetAllowedRedirectURIs([]string{redirect, "https://state-override"})
	p := testNewProvider(t, clientID, clientSecret, redirect, tp)

	validState, err := NewState(10*time.Second, redirect)
	require.NoError(t, err)

	expiredState, err := NewState(1*time.Nanosecond, redirect)
	require.NoError(t, err)

	allOptsState, err := NewState(
		10*time.Second,
		redirect,
		WithAudiences("state-override"),
		WithScopes("email", "profile"),
	)
	require.NoError(t, err)

	verifier, err := NewCodeVerifier()
	require.NoError(t, err)
	stWithPKCE, err := NewState(
		1*time.Minute,
		redirect,
		WithPKCE(verifier),
	)
	require.NoError(t, err)

	type args struct {
		ctx               context.Context
		s                 State
		authState         string
		authCode          string
		expectedNonce     string
		expectedAudiences []string
	}
	tests := []struct {
		name      string
		p         *Provider
		args      args
		wantErr   bool
		wantIsErr error
	}{
		{
			name: "valid",
			p:    p,
			args: args{
				ctx:       ctx,
				s:         validState,
				authState: validState.ID(),
				authCode:  "test-code",
			},
		},
		{
			name: "valid-all-opts-state",
			p:    p,
			args: args{
				ctx:               ctx,
				s:                 allOptsState,
				authState:         allOptsState.ID(),
				authCode:          "test-code",
				expectedAudiences: []string{"state-override"},
			},
		},
		{
			name: "PKCE",
			p:    p,
			args: args{
				ctx:               ctx,
				s:                 stWithPKCE,
				authState:         stWithPKCE.ID(),
				authCode:          "test-code",
				expectedAudiences: []string{"state-override"},
			},
		},
		{
			name:      "nil-config",
			p:         &Provider{},
			wantErr:   true,
			wantIsErr: ErrNilParameter,
		},
		{
			name: "states-don't-match",
			p:    p,
			args: args{
				ctx:       ctx,
				s:         validState,
				authState: "not-equal",
				authCode:  "test-code",
			},
			wantErr:   true,
			wantIsErr: ErrInvalidParameter,
		},
		{
			name: "expired-state",
			p:    p,
			args: args{
				ctx:       ctx,
				s:         expiredState,
				authState: expiredState.ID(),
				authCode:  "test-code",
			},
			wantErr:   true,
			wantIsErr: ErrInvalidParameter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			tp.SetExpectedAuthCode(tt.args.authCode)

			// default to the state's nonce...
			if tt.args.s != nil {
				tp.SetExpectedAuthNonce(tt.args.s.Nonce())
				if tt.args.s.PKCEVerifier() != nil {
					tp.SetPKCEVerifier(tt.args.s.PKCEVerifier())
				}
			}
			if tt.args.expectedNonce != "" {
				tp.SetExpectedAuthNonce(tt.args.expectedNonce)
			}
			if len(tt.args.expectedAudiences) != 0 {
				tp.SetCustomAudience(tt.args.expectedAudiences...)
				tp.SetCustomClaims(map[string]interface{}{"azp": clientID})
			}
			gotTk, err := tt.p.Exchange(tt.args.ctx, tt.args.s, tt.args.authState, tt.args.authCode)
			if tt.wantErr {
				require.Error(err)
				assert.Truef(errors.Is(err, tt.wantIsErr), "wanted \"%s\" but got \"%s\"", tt.wantIsErr, err)
				return
			}
			require.NoError(err)
			require.NotEmptyf(gotTk, "Provider.Exchange() = %v, wanted not nil", gotTk)
			assert.NotEmptyf(gotTk.IDToken(), "gotTk.IDToken() = %v, wanted not empty", gotTk.IDToken())
			assert.NotEmptyf(gotTk.AccessToken(), "gotTk.AccessToken() = %v, wanted not empty", gotTk.AccessToken())
			assert.Truef(gotTk.Valid(), "gotTk.Valid() = %v, wanted true", gotTk.Valid())
			assert.Truef(!gotTk.IsExpired(), "gotTk.Expired() = %v, wanted false", gotTk.IsExpired())
		})
	}

	t.Run("bad-expected-auth-code", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		code := "code-doesn't-match-state"
		tp.SetExpectedAuthCode(code)
		gotTk, err := p.Exchange(ctx, validState, validState.ID(), "bad-code")
		require.Error(err)
		assert.Truef(strings.Contains(err.Error(), "401 Unauthorized"), "wanted strings.Contains \"%s\" but got \"%s\"", "401 Unauthorized", err)
		assert.Empty(gotTk)
	})
	t.Run("omit-id-token", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		tp.SetOmitIDTokens(true)
		tp.SetExpectedAuthCode("valid-code")
		gotTk, err := p.Exchange(ctx, validState, validState.ID(), "valid-code")
		require.Error(err)
		assert.Truef(errors.Is(err, ErrMissingIDToken), "wanted \"%s\" but got \"%s\"", ErrMissingIDToken, err)
		assert.Empty(gotTk)
	})
	t.Run("omit-access-token", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		tp.SetOmitAccessTokens(true)
		defer tp.SetOmitAccessTokens(false)
		tp.SetExpectedAuthCode("valid-code")
		gotTk, err := p.Exchange(ctx, validState, validState.ID(), "valid-code")
		require.Error(err)
		assert.Nil(gotTk)
		assert.Truef(errors.Is(err, ErrMissingAccessToken), "wanted \"%s\" but got \"%s\"", ErrMissingAccessToken, err)
	})
	t.Run("expired-token", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		tp.SetOmitIDTokens(false)
		tp.SetExpectedAuthCode("valid-code")
		tp.SetExpectedExpiry(-1 * time.Minute)
		gotTk, err := p.Exchange(ctx, validState, validState.ID(), "valid-code")
		require.Error(err)
		assert.Truef(errors.Is(err, ErrExpiredToken), "wanted \"%s\" but got \"%s\"", ErrExpiredToken, err)
		assert.Empty(gotTk)
	})
}

func TestHTTPClient(t *testing.T) {
	// HTTPClient if mostly covered by other tests, but we need to make
	// sure we handle nil configs and invalid CA certs
	t.Parallel()
	t.Run("nil-config", func(t *testing.T) {
		p := &Provider{}
		c, err := p.HTTPClient()
		assert.Error(t, err)
		assert.Truef(t, errors.Is(err, ErrNilParameter), "wanted \"%s\" but got \"%s\"", ErrNilParameter, err)
		assert.Empty(t, c)
	})
	t.Run("bad-cert", func(t *testing.T) {
		p := &Provider{
			config: &Config{
				ProviderCA: "bad-cert",
			},
		}
		c, err := p.HTTPClient()
		require.Error(t, err)
		assert.Truef(t, errors.Is(err, ErrInvalidCACert), "wanted \"%s\" but got \"%s\"", ErrInvalidCACert, err)
		assert.Empty(t, c)
	})
	t.Run("check-transport", func(t *testing.T) {
		_, testCaPem := TestGenerateCA(t, []string{"localhost"})
		p := &Provider{
			config: &Config{
				ProviderCA: testCaPem,
			},
		}
		c, err := p.HTTPClient()
		require.NoError(t, err)
		assert.Equal(t, c.Transport, p.client.Transport)
	})
}

func TestProvider_UserInfo(t *testing.T) {
	ctx := context.Background()
	clientID := "test-client-id"
	clientSecret := "test-client-secret"
	redirect := "https://test-redirect"

	tp := StartTestProvider(t)
	tp.SetAllowedRedirectURIs([]string{redirect})
	p := testNewProvider(t, clientID, clientSecret, redirect, tp)

	defaultClaims := func() map[string]interface{} {
		return map[string]interface{}{
			"sub":           "alice@example.com",
			"advisor":       "Faythe",
			"dob":           "1978",
			"friend":        "bob",
			"nickname":      "A",
			"nosy-neighbor": "Eve",
		}
	}
	defaultSub := "alice@example.com"
	type args struct {
		tokenSource oauth2.TokenSource
		claims      interface{}
		sub         string
		opt         []Option
	}
	tests := []struct {
		name           string
		p              *Provider
		args           args
		providerClaims map[string]interface{}
		wantClaims     interface{}
		wantErr        bool
		wantIsErr      error
	}{
		{
			name: "valid",
			p:    p,
			args: args{
				tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: "dummy_access_token",
					Expiry:      time.Now().Add(10 * time.Second),
				}),
				claims: &map[string]interface{}{},
				sub:    defaultSub,
				opt:    []Option{WithAudiences(clientID)},
			},
			providerClaims: func() map[string]interface{} {
				c := defaultClaims()
				c["iss"] = tp.Addr()
				c["aud"] = []string{clientID}
				return c
			}(),
			wantClaims: func() *map[string]interface{} {
				c := defaultClaims()
				c["iss"] = tp.Addr()
				c["aud"] = []interface{}{clientID}
				return &c
			}(),
		},
		{
			name: "invalid-audiences",
			p:    p,
			args: args{
				tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: "dummy_access_token",
					Expiry:      time.Now().Add(10 * time.Second),
				}),
				claims: &map[string]interface{}{},
				sub:    defaultSub,
				opt:    []Option{WithAudiences(tp.Addr())},
			},
			wantErr:   true,
			wantIsErr: ErrInvalidAudience,
		},
		{
			name: "invalid-iss",
			p:    p,
			args: args{
				tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: "dummy_access_token",
					Expiry:      time.Now().Add(10 * time.Second),
				}),
				claims: &map[string]interface{}{},
				sub:    defaultSub,
			},
			providerClaims: func() map[string]interface{} {
				c := defaultClaims()
				c["iss"] = "bad-issuer"
				return c
			}(),
			wantErr:   true,
			wantIsErr: ErrInvalidIssuer,
		},
		{
			name: "invalid-sub",
			p:    p,
			args: args{
				tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: "dummy_access_token",
					Expiry:      time.Now().Add(10 * time.Second),
				}),
				claims: &map[string]interface{}{},
				sub:    "nobody",
			},
			wantErr:   true,
			wantIsErr: ErrInvalidSubject,
		},
		{
			name: "nil-tokensource",
			p:    p,
			args: args{
				tokenSource: nil,
				claims:      &map[string]interface{}{},
			},
			wantErr:   true,
			wantIsErr: ErrNilParameter,
		},
		{
			name: "nil-claims",
			p:    p,
			args: args{
				tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: "dummy_access_token",
					Expiry:      time.Now().Add(10 * time.Second),
				}),
				claims: nil,
			},
			wantErr:   true,
			wantIsErr: ErrNilParameter,
		},
		{
			name: "non-ptr-claims",
			p:    p,
			args: args{
				tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: "dummy_access_token",
					Expiry:      time.Now().Add(10 * time.Second),
				}),
				claims: map[string]interface{}{},
			},
			wantErr:   true,
			wantIsErr: ErrInvalidParameter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			if tt.providerClaims != nil {
				current := tp.UserInfoReply()
				tp.SetUserInfoReply(tt.providerClaims)
				defer tp.SetUserInfoReply(current)
			}
			err := p.UserInfo(ctx, tt.args.tokenSource, tt.args.sub, tt.args.claims, tt.args.opt...)
			if tt.wantErr {
				require.Error(err)
				assert.Truef(errors.Is(err, tt.wantIsErr), "wanted \"%s\" but got \"%s\"", tt.wantIsErr, err)
				return
			}
			require.NoError(err)
			require.NotEmptyf(tt.args.claims, "expected claims to not be empty")
			require.Equalf(tt.wantClaims, tt.args.claims, "wanted \"%s\" but got \"%s\"", tt.wantClaims, tt.args.claims)
		})
	}
	t.Run("failed-request", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: "dummy_access_token",
			Expiry:      time.Now().Add(10 * time.Second),
		})
		tp.SetDisableUserInfo(true)
		var claims interface{}
		err := p.UserInfo(ctx, tokenSource, "alice@example.com", &claims)
		require.Error(err)
		assert.Empty(claims)
		assert.True(errors.Is(err, ErrNotFound))
	})
}

func TestProvider_VerifyIDToken(t *testing.T) {
	t.Parallel()
	type keys struct {
		priv  crypto.PrivateKey
		pub   crypto.PublicKey
		alg   Alg
		keyID string
	}
	ctx := context.Background()
	clientID := "test-client-id"
	clientSecret := "test-client-secret"
	redirect := "https://test-redirect"

	tp := StartTestProvider(t)
	tp.SetAllowedRedirectURIs([]string{redirect})

	defaultProvider := testNewProvider(t, clientID, clientSecret, redirect, tp)
	defaultProvider.config.SupportedSigningAlgs = []Alg{ES256}

	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	defaultKeys := keys{priv: k, pub: &k.PublicKey, alg: ES256, keyID: "valid-ES256"}

	defaultState, err := NewState(1*time.Minute, "http://localhost")
	require.NoError(t, err)
	defaultClaims := func() map[string]interface{} {
		return map[string]interface{}{
			"sub":   "alice@bob.com",
			"aud":   []string{clientID},
			"nbf":   float64(time.Now().Unix()),
			"iat":   float64(time.Now().Unix()),
			"exp":   float64(time.Now().Add(1 * time.Minute).Unix()),
			"id":    "1",
			"nonce": defaultState.Nonce(),
		}
	}
	type args struct {
		keys           keys
		claims         map[string]interface{}
		state          State
		overrideIssuer string
	}
	tests := []struct {
		name      string
		p         *Provider
		args      args
		wantErr   bool
		wantIsErr error
	}{
		{
			name: "valid-ES256",
			p:    defaultProvider,
			args: args{
				keys:   defaultKeys,
				claims: defaultClaims(),
				state:  defaultState,
			},
		},
		{
			name: "nonces-not-equal",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["nonce"] = "not-equal"
					return c
				}(),
				state: defaultState,
			},
			wantErr:   true,
			wantIsErr: ErrInvalidNonce,
		},
		{
			name: "valid-with-state-audiences",
			p:    defaultProvider,
			args: args{
				keys:   defaultKeys,
				claims: defaultClaims(),
				state: func() State {
					aud := []string{tp.clientID, "second-aud"}
					s, err := NewState(1*time.Minute, "http://localhost", WithAudiences(aud...))
					s.nonce = defaultState.nonce
					require.NoError(t, err)
					return s
				}(),
			},
		},
		{
			name: "unsupported-alg",
			p: func() *Provider {
				p := testNewProvider(t, clientID, clientSecret, redirect, tp)
				p.config.SupportedSigningAlgs = []Alg{RS256}
				return p
			}(),
			args: args{
				keys: func() keys {
					k, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
					require.NoError(t, err)
					return keys{priv: k, pub: &k.PublicKey, alg: ES384, keyID: "valid-ES384"}
				}(),
				claims: defaultClaims(),
				state:  defaultState,
			},
			wantErr:   true,
			wantIsErr: ErrUnsupportedAlg,
		},
		{
			name: "bad-issuer",
			p:    defaultProvider,
			args: args{
				overrideIssuer: "bad-issuer",
				keys:           defaultKeys,
				claims:         defaultClaims(),
				state:          defaultState,
			},
			wantErr:   true,
			wantIsErr: ErrInvalidIssuer,
		},
		{
			name: "bad-nbf",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["nbf"] = float64(time.Now().Add(10 * time.Minute).Unix())
					return c
				}(),
				state: defaultState,
			},
			wantErr:   true,
			wantIsErr: ErrInvalidNotBefore,
		},
		{
			name: "bad-exp",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["exp"] = float64(time.Now().Add(-10 * time.Minute).Unix())
					return c
				}(),
				state: defaultState,
			},
			wantErr:   true,
			wantIsErr: ErrExpiredToken,
		},
		{
			name: "bad-iat",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["iat"] = float64(time.Now().Add(10 * time.Minute).Unix())
					return c
				}(),
				state: defaultState,
			},
			wantErr:   true,
			wantIsErr: ErrInvalidIssuedAt,
		},
		{
			name: "invalid-aud",
			p: func() *Provider {
				p := testNewProvider(t, clientID, clientSecret, redirect, tp)
				p.config.SupportedSigningAlgs = []Alg{ES256}
				p.config.Audiences = []string{"eve"}
				return p
			}(),
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["aud"] = []string{"alice", "bob"}
					return c
				}(),
				state: defaultState,
			},
			wantErr:   true,
			wantIsErr: ErrInvalidAudience,
		},
		{
			name: "multiple-aud-does-not-inc-client-id",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["aud"] = []string{"alice", "bob"}
					return c
				}(),
				state: defaultState,
			},
			wantErr:   true,
			wantIsErr: ErrInvalidAudience,
		},
		{
			name: "missing-azp-multi-aud",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["aud"] = []string{"alice", "bob", defaultProvider.config.ClientID}
					return c
				}(),
				state: defaultState,
			},
			wantErr:   true,
			wantIsErr: ErrInvalidAuthorizedParty,
		},
		{
			name: "invalid-azp-multi-aud",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["aud"] = []string{"alice", "bob", defaultProvider.config.ClientID}
					c["azp"] = "bob"
					return c
				}(),
				state: defaultState,
			},
			wantErr:   true,
			wantIsErr: ErrInvalidAuthorizedParty,
		},
		{
			name: "valid-azp-multi-aud",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["aud"] = []string{"alice", "bob", defaultProvider.config.ClientID}
					c["azp"] = defaultProvider.config.ClientID
					return c
				}(),
				state: defaultState,
			},
		},
		{
			name: "single-aud-missing-azp",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["aud"] = []string{"alice"}
					return c
				}(),
				state: defaultState,
			},
			wantErr:   true,
			wantIsErr: ErrInvalidAuthorizedParty,
		},
		{
			name: "single-aud-valid-azp",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["aud"] = []string{"alice"}
					c["azp"] = defaultProvider.config.ClientID
					return c
				}(),
				state: defaultState,
			},
		},
		{
			name: "valid-auth_time",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["auth_time"] = float64(time.Now().Unix())
					return c
				}(),
				state: func() State {
					s, err := NewState(1*time.Minute, "http://localhost", WithMaxAge(60*60))
					s.nonce = defaultState.nonce
					require.NoError(t, err)
					return s
				}(),
			},
		},
		{
			name: "exp-auth_time",
			p:    defaultProvider,
			args: args{
				keys: defaultKeys,
				claims: func() map[string]interface{} {
					c := defaultClaims()
					c["auth_time"] = float64(time.Now().Add(-1 * time.Hour).Unix())
					return c
				}(),
				state: func() State {
					s, err := NewState(1*time.Minute, "http://localhost", WithMaxAge(1))
					s.nonce = defaultState.nonce
					require.NoError(t, err)
					return s
				}(),
			},
			wantErr:   true,
			wantIsErr: ErrExpiredAuthTime,
		},
		{
			name: "missing-auth_time",
			p:    defaultProvider,
			args: args{
				keys:   defaultKeys,
				claims: defaultClaims(),
				state: func() State {
					s, err := NewState(1*time.Minute, "http://localhost", WithMaxAge(1))
					s.nonce = defaultState.nonce
					require.NoError(t, err)
					return s
				}(),
			},
			wantErr:   true,
			wantIsErr: ErrMissingClaim,
		},
		{
			name: "valid-ES384",
			p: func() *Provider {
				p := testNewProvider(t, clientID, clientSecret, redirect, tp)
				p.config.SupportedSigningAlgs = []Alg{ES384}
				return p
			}(),
			args: args{
				keys: func() keys {
					k, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
					require.NoError(t, err)
					return keys{priv: k, pub: &k.PublicKey, alg: ES384, keyID: "valid-ES384"}
				}(),
				claims: defaultClaims(),
				state:  defaultState,
			},
		},
		{
			name: "valid-ES512",
			p: func() *Provider {
				p := testNewProvider(t, clientID, clientSecret, redirect, tp)
				p.config.SupportedSigningAlgs = []Alg{ES512}
				return p
			}(),
			args: args{
				keys: func() keys {
					k, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
					require.NoError(t, err)
					return keys{priv: k, pub: &k.PublicKey, alg: ES512, keyID: "valid-ES512"}
				}(),
				claims: defaultClaims(),
				state:  defaultState,
			},
		},
		{
			name: "valid-RS256",
			p: func() *Provider {
				p := testNewProvider(t, clientID, clientSecret, redirect, tp)
				p.config.SupportedSigningAlgs = []Alg{RS256}
				return p
			}(),
			args: args{
				keys: func() keys {
					k, err := rsa.GenerateKey(rand.Reader, 2048)
					require.NoError(t, err)
					return keys{priv: k, pub: &k.PublicKey, alg: RS256, keyID: "valid-RS256"}
				}(),
				claims: defaultClaims(),
				state:  defaultState,
			},
		},
		{
			name: "valid-RS384",
			p: func() *Provider {
				p := testNewProvider(t, clientID, clientSecret, redirect, tp)
				p.config.SupportedSigningAlgs = []Alg{RS384}
				return p
			}(),
			args: args{
				keys: func() keys {
					k, err := rsa.GenerateKey(rand.Reader, 2048)
					require.NoError(t, err)
					return keys{priv: k, pub: &k.PublicKey, alg: RS384, keyID: "valid-RS384"}
				}(),
				claims: defaultClaims(),
				state:  defaultState,
			},
		},
		{
			name: "valid-RS512",
			p: func() *Provider {
				p := testNewProvider(t, clientID, clientSecret, redirect, tp)
				p.config.SupportedSigningAlgs = []Alg{RS512}
				return p
			}(),
			args: args{
				keys: func() keys {
					k, err := rsa.GenerateKey(rand.Reader, 2048)
					require.NoError(t, err)
					return keys{priv: k, pub: &k.PublicKey, alg: RS512, keyID: "valid-RS512"}
				}(),
				claims: defaultClaims(),
				state:  defaultState,
			},
		},
		{
			name: "valid-PS256",
			p: func() *Provider {
				p := testNewProvider(t, clientID, clientSecret, redirect, tp)
				p.config.SupportedSigningAlgs = []Alg{PS256}
				return p
			}(),
			args: args{
				keys: func() keys {
					k, err := rsa.GenerateKey(rand.Reader, 2048)
					require.NoError(t, err)
					return keys{priv: k, pub: &k.PublicKey, alg: PS256, keyID: "valid-PS256"}
				}(),
				claims: defaultClaims(),
				state:  defaultState,
			},
		},
		{
			name: "valid-PS384",
			p: func() *Provider {
				p := testNewProvider(t, clientID, clientSecret, redirect, tp)
				p.config.SupportedSigningAlgs = []Alg{PS384}
				return p
			}(),
			args: args{
				keys: func() keys {
					k, err := rsa.GenerateKey(rand.Reader, 2048)
					require.NoError(t, err)
					return keys{priv: k, pub: &k.PublicKey, alg: PS384, keyID: "valid-PS384"}
				}(),
				claims: defaultClaims(),
				state:  defaultState,
			},
		},
		{
			name: "valid-PS512",
			p: func() *Provider {
				p := testNewProvider(t, clientID, clientSecret, redirect, tp)
				p.config.SupportedSigningAlgs = []Alg{PS512}
				return p
			}(),
			args: args{
				keys: func() keys {
					k, err := rsa.GenerateKey(rand.Reader, 2048)
					require.NoError(t, err)
					return keys{priv: k, pub: &k.PublicKey, alg: PS512, keyID: "valid-PS512"}
				}(),
				claims: defaultClaims(),
				state:  defaultState,
			},
		},
		{
			name: "valid-EdDSA",
			p: func() *Provider {
				p := testNewProvider(t, clientID, clientSecret, redirect, tp)
				p.config.SupportedSigningAlgs = []Alg{EdDSA}
				return p
			}(),
			args: args{
				keys: func() keys {
					pub, priv, err := ed25519.GenerateKey(rand.Reader)
					require.NoError(t, err)
					// notice the pub key is not a pointer in this case!!!!
					return keys{priv: priv, pub: pub, alg: EdDSA, keyID: "valid-EdDSA"}
				}(),
				claims: defaultClaims(),
				state:  defaultState,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert, require := assert.New(t), require.New(t)
			tp.SetSigningKeys(tt.args.keys.priv, tt.args.keys.pub, tt.args.keys.alg, tt.args.keys.keyID)
			priv, pub, alg, _ := tp.SigningKeys()
			require.Equalf(tt.args.keys.priv, priv, "TestProvider priv key is invalid")
			require.Equalf(tt.args.keys.pub, pub, "TestProvider pub key is invalid")
			require.Equalf(tt.args.keys.alg, alg, "TestProvider alg key is invalid")
			switch {
			case tt.args.overrideIssuer != "":
				tt.args.claims["iss"] = tt.args.overrideIssuer
			default:
				tt.args.claims["iss"] = tt.p.config.Issuer
			}
			idToken := IDToken(TestSignJWT(t, tt.args.keys.priv, tt.args.keys.alg, tt.args.claims, []byte(tt.args.keys.keyID)))
			_, err := tt.p.VerifyIDToken(ctx, idToken, tt.args.state)
			if tt.wantErr {
				require.Error(err)
				if tt.wantIsErr != nil {
					assert.Truef(errors.Is(err, tt.wantIsErr), "wanted \"%s\" but got \"%s\"", tt.wantIsErr, err)
				}
				return
			}
			require.NoError(err)
		})
	}
	t.Run("bad-sig", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(err)
		c := defaultClaims()
		c["iss"] = defaultProvider.config.Issuer
		idToken := IDToken(TestSignJWT(t, k, ES256, c, []byte(defaultKeys.keyID)))
		_, err = defaultProvider.VerifyIDToken(ctx, idToken, defaultState)
		require.Error(err)
		assert.Truef(errors.Is(err, ErrInvalidSignature), "wanted \"%s\" but got \"%s\"", ErrInvalidSignature, err)
	})
	t.Run("empty-token", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		_, err := defaultProvider.VerifyIDToken(ctx, "", defaultState)
		require.Error(err)
		assert.Truef(errors.Is(err, ErrInvalidParameter), "wanted \"%s\" but got \"%s\"", ErrInvalidParameter, err)
	})
	t.Run("empty-nonce", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		s, err := NewState(1*time.Minute, "http://localhost")
		require.NoError(err)
		s.nonce = ""
		_, err = defaultProvider.VerifyIDToken(ctx, "token", s)
		require.Error(err)
		assert.Truef(errors.Is(err, ErrInvalidParameter), "wanted \"%s\" but got \"%s\"", ErrInvalidParameter, err)
	})
	t.Run("missing-and-disabled-jwks", func(t *testing.T) {
		assert, require := assert.New(t), require.New(t)
		claims := defaultClaims()
		claims["iss"] = defaultProvider.config.Issuer
		idToken := IDToken(TestSignJWT(t, defaultKeys.priv, defaultKeys.alg, claims, []byte(defaultKeys.keyID)))
		func() {
			tp.SetDisableJWKs(true)
			defer tp.SetDisableJWKs(false)
			_, err := defaultProvider.VerifyIDToken(ctx, idToken, defaultState)
			require.Error(err)
			assert.Truef(errors.Is(err, ErrInvalidJWKs), "wanted \"%s\" but got \"%s\"", ErrInvalidJWKs, err)
		}()
		idToken = IDToken(TestSignJWT(t, defaultKeys.priv, defaultKeys.alg, claims, []byte(defaultKeys.keyID)))
		func() {
			tp.SetInvalidJWKS(true)
			defer tp.SetInvalidJWKS(false)
			_, err = defaultProvider.VerifyIDToken(ctx, idToken, defaultState)
			require.Error(err)
			assert.Truef(errors.Is(err, ErrInvalidJWKs), "wanted \"%s\" but got \"%s\"", ErrInvalidJWKs, err)
		}()
	})
}

func TestProvider_validRedirect(t *testing.T) {
	tests := []struct {
		uri      string
		allowed  []string
		expected error
	}{
		// valid
		{"https://example.com", []string{"https://example.com"}, nil},
		{"https://example.com:5000", []string{"a", "b", "https://example.com:5000"}, nil},
		{"https://example.com/a/b/c", []string{"a", "b", "https://example.com/a/b/c"}, nil},
		{"https://localhost:9000", []string{"a", "b", "https://localhost:5000"}, nil},
		{"https://127.0.0.1:9000", []string{"a", "b", "https://127.0.0.1:5000"}, nil},
		{"https://[::1]:9000", []string{"a", "b", "https://[::1]:5000"}, nil},
		{"https://[::1]:9000/x/y?r=42", []string{"a", "b", "https://[::1]:5000/x/y?r=42"}, nil},
		{"https://example.com", []string{}, nil},

		// invalid
		{"http://example.com", []string{"a", "b", "https://example.com"}, ErrUnauthorizedRedirectURI},
		{"https://example.com:9000", []string{"a", "b", "https://example.com:5000"}, ErrUnauthorizedRedirectURI},
		{"https://[::2]:9000", []string{"a", "b", "https://[::2]:5000"}, ErrUnauthorizedRedirectURI},
		{"https://localhost:5000", []string{"a", "b", "https://127.0.0.1:5000"}, ErrUnauthorizedRedirectURI},
		{"https://localhost:5000", []string{"a", "b", "https://127.0.0.1:5000"}, ErrUnauthorizedRedirectURI},
		{"https://localhost:5000", []string{"a", "b", "http://localhost:5000"}, ErrUnauthorizedRedirectURI},
		{"https://[::1]:5000/x/y?r=42", []string{"a", "b", "https://[::1]:5000/x/y?r=43"}, ErrUnauthorizedRedirectURI},

		// extra invalid
		{"%%%%%%%%%%%", []string{"%%%%%%%%%%%"}, ErrInvalidParameter},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("uri=%q allowed=%#v", tt.uri, tt.allowed), func(t *testing.T) {
			p := &Provider{
				config: &Config{
					AllowedRedirectURLs: tt.allowed,
				},
			}
			p.config.AllowedRedirectURLs = tt.allowed
			require.Truef(t, errors.Is(p.validRedirect(tt.uri), tt.expected), "got [%v] and expected [%v]", p.validRedirect(tt.uri), tt.expected)
		})
	}
}
