# cap

`cap` (collection of authentication packages) provides a collection of related
packages which enable support for OIDC, JWT Verification and Distributed Claims.

**Please note**: We take security and our users' trust very seriously. If you 
believe you have found a security issue, please [responsibly
disclose](https://www.hashicorp.com/security#vulnerability-reporting) by
contacting us at  security@hashicorp.com.

## Contributing

Thank you for your interest in contributing! Please refer to
[CONTRIBUTING.md](https://github.com/hashicorp/cap/blob/main/CONTRIBUTING.md)
for guidance. 

<hr>

### [`oidc package`](./oidc) 
[![Go Reference](https://pkg.go.dev/badge/github.com/hashicorp/cap/oidc.svg)](https://pkg.go.dev/github.com/hashicorp/cap/oidc)
 
 A package for writing clients that integrate with OIDC Providers. Primary types provided by the
 package are: 
 1. Request
 2. Token
 3. Config
 4. Provider 

The package also provides callbacks (in the form of http.HandlerFunc) for
handling OIDC provider responses to authorization code flow (with optional PKCE)
and implicit flow authentication attempts.
<hr>

Example of a provider using an authorization code flow:
```go
// Create a new provider config
pc, err := oidc.NewConfig(
    "http://your-issuer.com/",
    "your_client_id",
    "your_client_secret",
    []oidc.Alg{oidc.RS256},
    []string{"https://your_redirect_url"},
)
if err != nil {
    // handle error
}

// Create a provider
p, err := oidc.NewProvider(pc)
if err != nil {
    // handle error
}
defer p.Done()


// Create a Request for a user's authorization code flow authentication attempt, 
// with a 2 min timeout for  completion. 
oidcRequest, err := oidc.NewRequest(2 * time.Minute, "https://your_redirect_url")
if err != nil {
    // handle error
}


// Create an auth URL
authURL, err := p.AuthURL(ctx, oidcRequest)
if err != nil {
    // handle error
}
fmt.Println("open url to kick-off authentication: ", authURL)
```

Create a http.Handler for OIDC authentication response redirects.
```go
func NewHandler(ctx context.Context, p *oidc.Provider, r callback.RequestReader) (http.HandlerFunc, error)
    if p == nil { 
        // handle error
    }
    if rw == nil {
        // handle error
    }
    return func(w http.ResponseWriter, req *http.Request) {
        oidcRequest, err := rw.Read(ctx, req.FormValue("state"))
        if err != nil {
            // handle error
        }
        // Exchange(...) will verify the tokens before returning. 
        token, err := p.Exchange(ctx, oidcRequest, req.FormValue("state"), req.FormValue("code"))
        if err != nil {
            // handle error
        }
        var claims map[string]interface{}
        if err := token.IDToken().Claims(&claims); err != nil {
            // handle error
        }

        // Get the user's claims via the provider's UserInfo endpoint
        var infoClaims map[string]interface{}
        err = p.UserInfo(ctx, token.StaticTokenSource(), claims["sub"].(string), &infoClaims)
        if err != nil {
            // handle error
        }
        resp := struct {
		    IDTokenClaims  map[string]interface{}
		    UserInfoClaims map[string]interface{}
		}{claims, infoClaims}
		enc := json.NewEncoder(w)
		if err := enc.Encode(resp); err != nil {
			// handle error
        }
    }
}
```
  
 
