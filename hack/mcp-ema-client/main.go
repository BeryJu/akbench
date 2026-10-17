// Command mcp-ema-client drives MCP Enterprise-Managed Authorization
// (SEP-990) end to end against akbench's MCP endpoint, as a test client for
// identity providers implementing ID-JAG (Identity Assertion JWT
// Authorization Grant) - authentik in particular. akbench is the
// authorization server:
//
//  1. discovery: an unauthenticated MCP request's 401 challenge -> RFC 9728
//     protected resource metadata -> the MCP authorization server
//  2. OIDC login at the IdP (authorization code + PKCE, local callback), or
//     a given -id-token
//  3. RFC 8693 token exchange at the IdP: ID token -> ID-JAG
//  4. RFC 7523 JWT bearer grant at akbench's authorization server: ID-JAG ->
//     access token
//  5. MCP tools/list (and optionally one tools/call) with that token
//
// Every token is decoded and printed (signatures are not verified - that's
// the servers' job) and the ID-JAG is checked against the claims the draft
// requires. The grants are raw form POSTs rather than the SDK's
// EnterpriseHandler so a server's error responses show up verbatim.
//
//	go run ./hack/mcp-ema-client -mcp-url https://localhost:8443/api/v1/mcp -insecure \
//	  -idp-issuer https://idp.example.com/application/o/enterprise/ -idp-client-id ... \
//	  -mcp-client-id ... -mcp-client-secret ...
package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/auth/extauth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const (
	grantTypeTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"
	grantTypeJWTBearer     = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	tokenTypeIDToken       = "urn:ietf:params:oauth:token-type:id_token"
	tokenTypeIDJAG         = "urn:ietf:params:oauth:token-type:id-jag"
	// idJAGType is the JWT typ header an ID-JAG must carry.
	idJAGType = "oauth-id-jag+jwt"
)

type options struct {
	mcpURL          string
	resource        string
	mcpAuthServer   string
	mcpClientID     string
	mcpClientSecret string
	mcpScopes       string

	idpIssuer       string
	idpClientID     string
	idpClientSecret string
	idToken         string
	redirectURL     string
	loginScopes     string
	loginHint       string

	accessToken string
	call        string
	args        string
	insecure    bool
}

func main() {
	var o options
	flag.StringVar(&o.mcpURL, "mcp-url", "", "akbench MCP endpoint, e.g. https://localhost:8443/api/v1/mcp (required)")
	flag.StringVar(&o.resource, "resource", "",
		"MCP resource identifier; defaults to the protected resource metadata's, else -mcp-url")
	flag.StringVar(&o.mcpAuthServer, "mcp-auth-server", "",
		"MCP authorization server issuer; discovered from the resource metadata when unset")
	flag.StringVar(&o.mcpClientID, "mcp-client-id", "", "client ID registered at the MCP authorization server")
	flag.StringVar(&o.mcpClientSecret, "mcp-client-secret", os.Getenv("MCP_CLIENT_SECRET"),
		"its client secret, if confidential ($MCP_CLIENT_SECRET)")
	flag.StringVar(&o.mcpScopes, "mcp-scopes", "", "space-separated scopes to request for the MCP server")
	flag.StringVar(&o.idpIssuer, "idp-issuer", "", "enterprise IdP issuer URL")
	flag.StringVar(&o.idpClientID, "idp-client-id", "", "client ID registered at the IdP (login and token exchange)")
	flag.StringVar(&o.idpClientSecret, "idp-client-secret", os.Getenv("IDP_CLIENT_SECRET"),
		"its client secret, if confidential ($IDP_CLIENT_SECRET)")
	flag.StringVar(&o.idToken, "id-token", os.Getenv("ID_TOKEN"),
		"skip the IdP login and exchange this ID token ($ID_TOKEN)")
	flag.StringVar(&o.redirectURL, "redirect-url", "http://127.0.0.1:8976/callback",
		"login redirect URI registered at the IdP; served locally")
	flag.StringVar(&o.loginScopes, "login-scopes", "openid profile email", "scopes requested at IdP login")
	flag.StringVar(&o.loginHint, "login-hint", "", "login_hint for the IdP")
	flag.StringVar(&o.accessToken, "access-token", "", "skip steps 1-4 and call MCP with this access token")
	flag.StringVar(&o.call, "call", "", "also call this MCP tool, e.g. list_test_cases")
	flag.StringVar(&o.args, "args", "{}", "JSON arguments for -call")
	flag.BoolVar(&o.insecure, "insecure", false, "skip TLS verification (akbench's default self-signed certificate)")
	flag.Parse()

	if err := run(context.Background(), &o); err != nil {
		fmt.Fprintln(os.Stderr, "\nFAILED:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, o *options) error {
	if o.mcpURL == "" {
		return errors.New("-mcp-url is required")
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if o.insecure {
		// Opt-in, for akbench's self-signed dev certificate.
		tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // see above
		httpClient.Transport = &http.Transport{TLSClientConfig: tlsCfg}
	}

	accessToken := o.accessToken
	if accessToken == "" {
		var err error
		if accessToken, err = authorize(ctx, o, httpClient); err != nil {
			return err
		}
	}
	if err := callMCP(ctx, o, httpClient, accessToken); err != nil {
		return err
	}
	if failedChecks > 0 {
		return fmt.Errorf("%d checks failed", failedChecks)
	}
	fmt.Println("\nAll checks passed")
	return nil
}

func authorize(ctx context.Context, o *options, httpClient *http.Client) (string, error) {
	if o.idpIssuer == "" || o.idpClientID == "" || o.mcpClientID == "" {
		return "", errors.New("-idp-issuer, -idp-client-id and -mcp-client-id are required (or pass -access-token)")
	}

	step("1. Discover the MCP authorization server")
	resource, authServer, err := discover(ctx, o, httpClient)
	if err != nil {
		return "", err
	}
	fmt.Printf("   resource:             %s\n   authorization server: %s\n", resource, authServer)

	step("2. Obtain an ID token from the IdP")
	idToken := o.idToken
	if idToken == "" {
		if idToken, err = login(ctx, o, httpClient); err != nil {
			return "", err
		}
	} else {
		fmt.Println("   using -id-token")
	}
	printJWT("ID token", idToken)

	idpMeta, err := auth.GetAuthServerMetadata(ctx, o.idpIssuer, httpClient)
	if err != nil || idpMeta == nil {
		return "", fmt.Errorf("discover IdP metadata: %v", err)
	}
	mcpMeta, err := auth.GetAuthServerMetadata(ctx, authServer, httpClient)
	if err != nil || mcpMeta == nil {
		return "", fmt.Errorf("discover MCP authorization server metadata: %v", err)
	}
	checkGrant("IdP", idpMeta, grantTypeTokenExchange)
	checkGrant("MCP authorization server", mcpMeta, grantTypeJWTBearer)

	step("3. Token exchange at the IdP: ID token -> ID-JAG (RFC 8693)")
	form := url.Values{
		"grant_type":           {grantTypeTokenExchange},
		"requested_token_type": {tokenTypeIDJAG},
		"audience":             {authServer},
		"resource":             {resource},
		"subject_token":        {idToken},
		"subject_token_type":   {tokenTypeIDToken},
	}
	if o.mcpScopes != "" {
		form.Set("scope", o.mcpScopes)
	}
	exchanged, err := postToken(ctx, httpClient, idpMeta.TokenEndpoint, form, o.idpClientID, o.idpClientSecret)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	check(exchanged["issued_token_type"] == tokenTypeIDJAG,
		"issued_token_type is %s (got %v)", tokenTypeIDJAG, exchanged["issued_token_type"])
	check(exchanged["token_type"] == "N_A", "token_type is N_A (got %v)", exchanged["token_type"])
	idJAG, _ := exchanged["access_token"].(string)
	if idJAG == "" {
		return "", errors.New("token exchange response has no access_token (the ID-JAG)")
	}
	header, claims := printJWT("ID-JAG", idJAG)
	checkIDJAG(header, claims, o, authServer, resource)

	step("4. JWT bearer grant at the MCP authorization server: ID-JAG -> access token (RFC 7523)")
	form = url.Values{"grant_type": {grantTypeJWTBearer}, "assertion": {idJAG}}
	if o.mcpScopes != "" {
		form.Set("scope", o.mcpScopes)
	}
	granted, err := postToken(ctx, httpClient, mcpMeta.TokenEndpoint, form, o.mcpClientID, o.mcpClientSecret)
	if err != nil {
		return "", fmt.Errorf("JWT bearer grant: %w", err)
	}
	accessToken, _ := granted["access_token"].(string)
	if accessToken == "" {
		return "", errors.New("JWT bearer grant response has no access_token")
	}
	check(strings.EqualFold(fmt.Sprint(granted["token_type"]), "bearer"),
		"token_type is Bearer (got %v)", granted["token_type"])
	_, atClaims := printJWT("access token", accessToken)
	if atClaims != nil {
		check(audienceContains(atClaims["aud"], resource), "access token aud contains the resource %s", resource)
	}
	return accessToken, nil
}

// discover reads the MCP endpoint's 401 challenge and the protected resource
// metadata it points to. -resource/-mcp-auth-server override what's found.
func discover(ctx context.Context, o *options, httpClient *http.Client) (resource, authServer string, err error) {
	resource, authServer = o.resource, o.mcpAuthServer
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.mcpURL, strings.NewReader(`{}`))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	_ = resp.Body.Close()
	fmt.Printf("   unauthenticated request: %s\n", resp.Status)
	challenges, err := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
	if err != nil {
		return "", "", fmt.Errorf("parse WWW-Authenticate: %w", err)
	}
	var metadataURL string
	for _, c := range challenges {
		if c.Scheme == "bearer" && c.Params["resource_metadata"] != "" {
			metadataURL = c.Params["resource_metadata"]
		}
	}
	check(resp.StatusCode == http.StatusUnauthorized && metadataURL != "",
		"401 with a resource_metadata challenge (is akbench running with --mcp-resource-url?)")
	if metadataURL == "" {
		if resource == "" || authServer == "" {
			return "", "", errors.New("no resource metadata to discover from; pass -resource and -mcp-auth-server")
		}
		return resource, authServer, nil
	}

	want := resource
	if want == "" {
		want = o.mcpURL
	}
	prm, err := oauthex.GetProtectedResourceMetadata(ctx, metadataURL, want, httpClient)
	if err != nil {
		return "", "", err
	}
	if authServer == "" {
		if len(prm.AuthorizationServers) == 0 {
			return "", "", errors.New("resource metadata lists no authorization_servers")
		}
		authServer = prm.AuthorizationServers[0]
	}
	return prm.Resource, authServer, nil
}

// login runs the OIDC authorization code flow at the IdP, receiving the code
// on a local listener at -redirect-url.
func login(ctx context.Context, o *options, httpClient *http.Client) (string, error) {
	redirect, err := url.Parse(o.redirectURL)
	if err != nil {
		return "", fmt.Errorf("-redirect-url: %w", err)
	}
	var creds = &oauthex.ClientCredentials{ClientID: o.idpClientID}
	if o.idpClientSecret != "" {
		creds.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: o.idpClientSecret}
	}
	token, err := extauth.PerformOIDCLogin(ctx, &extauth.OIDCLoginConfig{
		IssuerURL:   o.idpIssuer,
		Credentials: creds,
		RedirectURL: o.redirectURL,
		Scopes:      strings.Fields(o.loginScopes),
		LoginHint:   o.loginHint,
		HTTPClient:  httpClient,
	}, func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		return awaitCallback(ctx, redirect, args.URL)
	})
	if err != nil {
		return "", err
	}
	idToken, _ := token.Extra("id_token").(string)
	if idToken == "" {
		return "", errors.New("IdP token response has no id_token")
	}
	return idToken, nil
}

func awaitCallback(ctx context.Context, redirect *url.URL, authURL string) (*auth.AuthorizationResult, error) {
	listener, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		return nil, fmt.Errorf("listen for the login callback: %w", err)
	}
	results := make(chan *auth.AuthorizationResult, 1)
	errs := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != redirect.Path {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			errs <- fmt.Errorf("IdP returned %s: %s", e, q.Get("error_description"))
			_, _ = io.WriteString(w, "Login failed, see the terminal.")
			return
		}
		results <- &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}
		_, _ = io.WriteString(w, "Logged in, you can close this tab.")
	})
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: handler}
	go func() { _ = srv.Serve(listener) }()
	defer func() { _ = srv.Close() }()

	fmt.Printf("   open this URL to log in:\n   %s\n", authURL)
	openBrowser(authURL)
	select {
	case res := <-results:
		return res, nil
	case err := <-errs:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func openBrowser(u string) {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "linux":
		cmd = "xdg-open"
	default:
		return
	}
	_ = exec.Command(cmd, u).Start() //nolint:gosec // the IdP's own authorization URL
}

// postToken POSTs a grant to a token endpoint with client_secret_post client
// authentication and returns the JSON response, or an error carrying the
// server's error response verbatim.
func postToken(
	ctx context.Context, httpClient *http.Client, endpoint string, form url.Values, clientID, clientSecret string,
) (map[string]any, error) {
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	fmt.Printf("   POST %s\n", endpoint)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode response %q: %w", body, err)
	}
	redacted := make(map[string]any, len(out))
	for k, v := range out {
		if s, ok := v.(string); ok && strings.HasSuffix(k, "_token") && len(s) > 16 {
			v = s[:16] + "..."
		}
		redacted[k] = v
	}
	printJSON("   response", redacted)
	return out, nil
}

// checkIDJAG checks the ID-JAG's header and claims against what the draft
// (draft-ietf-oauth-identity-assertion-authz-grant) and SEP-990 require.
func checkIDJAG(header, claims map[string]any, o *options, authServer, resource string) {
	if claims == nil {
		check(false, "ID-JAG is a JWT")
		return
	}
	check(header["typ"] == idJAGType, "ID-JAG typ header is %s (got %v)", idJAGType, header["typ"])
	check(trimSlash(fmt.Sprint(claims["iss"])) == trimSlash(o.idpIssuer),
		"iss is the IdP %s (got %v)", o.idpIssuer, claims["iss"])
	check(audienceContains(claims["aud"], authServer),
		"aud is the MCP authorization server %s (got %v)", authServer, claims["aud"])
	check(claims["client_id"] == o.mcpClientID,
		"client_id is the MCP client %s (got %v)", o.mcpClientID, claims["client_id"])
	check(audienceContains(claims["resource"], resource), "resource is %s (got %v)", resource, claims["resource"])
	for _, c := range []string{"sub", "jti", "exp", "iat"} {
		check(claims[c] != nil, "%s claim present", c)
	}
	if o.mcpScopes != "" {
		check(claims["scope"] != nil, "scope claim present (got %v)", claims["scope"])
	}
}

func checkGrant(name string, meta *oauthex.AuthServerMeta, grant string) {
	if len(meta.GrantTypesSupported) == 0 {
		fmt.Printf("   ?    %s doesn't list grant_types_supported\n", name)
		return
	}
	check(slices.Contains(meta.GrantTypesSupported, grant), "%s advertises %s", name, grant)
}

func callMCP(ctx context.Context, o *options, httpClient *http.Client, accessToken string) error {
	step("5. Call the MCP server")
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	authed := &http.Client{Timeout: httpClient.Timeout, Transport: bearer{token: accessToken, base: base}}
	client := mcp.NewClient(&mcp.Implementation{Name: "akbench-mcp-ema-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: o.mcpURL, HTTPClient: authed, DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, t := range tools.Tools {
		names = append(names, t.Name)
	}
	fmt.Printf("   %d tools: %s\n", len(names), strings.Join(names, ", "))

	if o.call == "" {
		return nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(o.args), &args); err != nil {
		return fmt.Errorf("-args: %w", err)
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: o.call, Arguments: args})
	if err != nil {
		return fmt.Errorf("tools/call %s: %w", o.call, err)
	}
	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			fmt.Println(text.Text)
		}
	}
	if res.IsError {
		return fmt.Errorf("tool %s returned an error", o.call)
	}
	return nil
}

type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

// printJWT decodes and prints a JWT's header and claims, without verifying
// it. Both are nil when raw isn't a JWT.
func printJWT(name, raw string) (header, claims map[string]any) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		fmt.Printf("   %s is not a JWT (%d parts)\n", name, len(parts))
		return nil, nil
	}
	decode := func(part string) map[string]any {
		b, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil {
			return nil
		}
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			return nil
		}
		return m
	}
	header, claims = decode(parts[0]), decode(parts[1])
	printJSON("   "+name+" header", header)
	printJSON("   "+name+" claims", claims)
	return header, claims
}

func printJSON(label string, v any) {
	b, _ := json.MarshalIndent(v, "   ", "  ")
	fmt.Printf("%s: %s\n", label, b)
}

func step(title string) { fmt.Printf("\n== %s\n", title) }

// failedChecks counts check failures, so a run that got a token but saw a
// non-conforming response still exits non-zero.
var failedChecks int

func check(ok bool, format string, args ...any) {
	mark := "ok  "
	if !ok {
		mark = "FAIL"
		failedChecks++
	}
	fmt.Printf("   %s %s\n", mark, fmt.Sprintf(format, args...))
}

// audienceContains reports whether a JWT aud-style claim (a string or an
// array of strings) contains want.
func audienceContains(claim any, want string) bool {
	switch v := claim.(type) {
	case string:
		return v == want
	case []any:
		for _, item := range v {
			if item == want {
				return true
			}
		}
	}
	return false
}

func trimSlash(s string) string { return strings.TrimSuffix(s, "/") }
