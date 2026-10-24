// OIDC federated-credential assertion providers.
//
// The native azurerm backend accepts a federated token three ways: a token
// file (workload identity), a GitHub Actions request URL, and an Azure DevOps
// service-connection request URL. Supporting only the token file would make a
// CI configuration that works during `tofu init` fail during Terragrunt's own
// lifecycle operations, so all three are resolved here.

package azurehelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gruntwork-io/terragrunt/internal/venv"
)

// oidcExchangeAudience is the audience Microsoft Entra expects for a
// federated credential assertion.
const oidcExchangeAudience = "api://AzureADTokenExchange"

// adoPipelineOIDCAPIVersion is the Azure DevOps API version the native
// backend's Azure DevOps authorizer requests the federated token with.
const adoPipelineOIDCAPIVersion = "7.1"

// maxOIDCTokenResponseBytes bounds the token-endpoint response so a
// misconfigured URL cannot stream an unbounded body into memory.
const maxOIDCTokenResponseBytes = 1 << 20

// assertionProvider returns a federated credential assertion (a JWT) for
// Microsoft Entra to exchange for an access token.
type assertionProvider func(ctx context.Context) (string, error)

// oidcAssertionProvider resolves the assertion source from the environment,
// mirroring the native azurerm backend's precedence: an Azure DevOps service
// connection wins, then an explicitly configured request URL, then GitHub
// Actions, then Azure DevOps. It returns nil when no request-URL flow is
// configured, leaving the token-file (workload identity) path to handle it.
func oidcAssertionProvider(v *venv.Venv) assertionProvider {
	// Azure DevOps workload identity federation through a service connection,
	// requested from the same variables as the native backend's Azure DevOps
	// authorizer.
	serviceConnectionID := firstEnv(v.Env, "ARM_ADO_PIPELINE_SERVICE_CONNECTION_ID", "ARM_OIDC_AZURE_SERVICE_CONNECTION_ID")
	if requestURL := firstEnv(v.Env, "ARM_OIDC_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_URL", "SYSTEM_OIDCREQUESTURI"); serviceConnectionID != "" && requestURL != "" {
		token := firstEnv(v.Env, "ARM_OIDC_REQUEST_TOKEN", "ACTIONS_ID_TOKEN_REQUEST_TOKEN", "SYSTEM_ACCESSTOKEN")

		return func(ctx context.Context) (string, error) {
			return fetchADOPipelineAssertion(ctx, v, requestURL, token, serviceConnectionID)
		}
	}

	// GitHub Actions injects both variables into every job that requests the
	// id-token permission.
	if requestURL := firstEnv(v.Env, "ARM_OIDC_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_URL"); requestURL != "" {
		token := firstEnv(v.Env, "ARM_OIDC_REQUEST_TOKEN", "ACTIONS_ID_TOKEN_REQUEST_TOKEN")

		return func(ctx context.Context) (string, error) {
			return fetchOIDCAssertion(ctx, v, requestURL, token, "value")
		}
	}

	// Azure DevOps workload identity federation.
	if requestURL := firstEnv(v.Env, "SYSTEM_OIDCREQUESTURI"); requestURL != "" {
		token := firstEnv(v.Env, "SYSTEM_ACCESSTOKEN")

		return func(ctx context.Context) (string, error) {
			return fetchOIDCAssertion(ctx, v, requestURL, token, "oidcToken")
		}
	}

	return nil
}

// fetchOIDCAssertion requests a federated token from requestURL, authorizing
// with bearerToken, and reads the assertion out of the named JSON field.
func fetchOIDCAssertion(ctx context.Context, v *venv.Venv, requestURL, bearerToken, field string) (string, error) {
	if bearerToken == "" {
		return "", &OIDCRequestTokenMissingError{RequestURL: requestURL}
	}

	parsed, err := url.Parse(requestURL)
	if err != nil {
		return "", fmt.Errorf("parsing OIDC request url: %w", err)
	}

	// The audience identifies Entra as the intended consumer of the assertion.
	query := parsed.Query()
	query.Set("audience", oidcExchangeAudience)
	parsed.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", fmt.Errorf("building OIDC token request: %w", err)
	}

	return requestOIDCAssertion(v, req, bearerToken, field)
}

// fetchADOPipelineAssertion requests a federated token for an Azure DevOps
// service connection the way the native backend's Azure DevOps authorizer
// does: a POST that adds the API version, the connection and the audience
// only when requestURL lacks them, read from oidcToken.
func fetchADOPipelineAssertion(ctx context.Context, v *venv.Venv, requestURL, bearerToken, serviceConnectionID string) (string, error) {
	if bearerToken == "" {
		return "", &OIDCRequestTokenMissingError{RequestURL: requestURL}
	}

	parsed, err := url.Parse(requestURL)
	if err != nil {
		return "", fmt.Errorf("parsing OIDC request url: %w", err)
	}

	query := parsed.Query()
	for key, value := range map[string]string{
		"api-version":         adoPipelineOIDCAPIVersion,
		"serviceConnectionId": serviceConnectionID,
		"audience":            oidcExchangeAudience,
	} {
		if query.Get(key) == "" {
			query.Set(key, value)
		}
	}

	parsed.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), http.NoBody)
	if err != nil {
		return "", fmt.Errorf("building OIDC token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	return requestOIDCAssertion(v, req, bearerToken, "oidcToken")
}

// requestOIDCAssertion sends req with bearerToken and reads the assertion out
// of the named JSON field of the response.
func requestOIDCAssertion(v *venv.Venv, req *http.Request, bearerToken, field string) (_ string, err error) {
	v.RequireHTTP()

	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Accept", "application/json")

	resp, err := v.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting OIDC token: %w", err)
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing OIDC token response: %w", closeErr))
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOIDCTokenResponseBytes))
	if err != nil {
		return "", fmt.Errorf("reading OIDC token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", &OIDCTokenRequestFailedError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(body)),
		}
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decoding OIDC token response: %w", err)
	}

	assertion, _ := payload[field].(string)
	if assertion == "" {
		return "", &OIDCTokenFieldMissingError{Field: field}
	}

	return assertion, nil
}
