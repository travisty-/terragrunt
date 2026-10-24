package config

import (
	"maps"
	"testing"

	"github.com/gruntwork-io/terragrunt/internal/remotestate"
	"github.com/gruntwork-io/terragrunt/internal/remotestate/backend"
	"github.com/gruntwork-io/terragrunt/internal/venv"
	"github.com/stretchr/testify/assert"
)

// TestAzureDirectStateReadSupportedADOPipeline covers Azure DevOps workload
// identity federation through a service connection, which the native backends
// and azurehelper all authenticate the same way, against the variables that
// would make one of them choose a different source.
func TestAzureDirectStateReadSupportedADOPipeline(t *testing.T) {
	t.Parallel()

	// An Azure DevOps job whose auth provider sets the request variables of the
	// azurerm backend, the azapi connection variable, and the Azure SDK
	// variables that DefaultAzureCredential providers read.
	adoEnv := map[string]string{
		"ARM_USE_OIDC":                           "true",
		"ARM_CLIENT_ID":                          "client",
		"ARM_TENANT_ID":                          "tenant",
		"ARM_SUBSCRIPTION_ID":                    "subscription",
		"ARM_ADO_PIPELINE_SERVICE_CONNECTION_ID": "connection",
		"ARM_OIDC_AZURE_SERVICE_CONNECTION_ID":   "connection",
		"ARM_OIDC_REQUEST_URL":                   "https://tokens.example/oidc?serviceConnectionId=connection",
		"ARM_OIDC_REQUEST_TOKEN":                 "request-token",
		"SYSTEM_OIDCREQUESTURI":                  "https://dev.azure.example/oidc",
		"SYSTEM_ACCESSTOKEN":                     "job-token",
		"AZURE_CLIENT_ID":                        "client",
		"AZURE_TENANT_ID":                        "tenant",
		"AZURE_FEDERATED_TOKEN_FILE":             "/var/run/token",
	}

	tests := []struct {
		set    map[string]string
		config map[string]any
		name   string
		unset  []string
		want   bool
	}{
		{
			name: "service connection request remains direct",
			want: true,
		},
		{
			name:  "pipeline request uri alone remains direct",
			unset: []string{"ARM_OIDC_REQUEST_URL", "ARM_OIDC_REQUEST_TOKEN"},
			want:  true,
		},
		{
			name:   "use_oidc in config remains direct",
			unset:  []string{"ARM_USE_OIDC"},
			config: map[string]any{"use_oidc": true},
			want:   true,
		},
		{
			name: "use_azuread_auth alias that only azurehelper reads falls back",
			set:  map[string]string{"ARM_USE_AZUREAD_AUTH": "true"},
		},
		{
			name: "token file that Terraform prefers falls back",
			set:  map[string]string{"ARM_OIDC_TOKEN_FILE_PATH": "/var/run/token"},
		},
		{
			name: "static token that Terraform prefers falls back",
			set:  map[string]string{"ARM_OIDC_TOKEN": "assertion"},
		},
		{
			name:   "token file in config falls back",
			config: map[string]any{"oidc_token_file_path": "/var/run/token"},
		},
		{
			name: "connection variable that only OpenTofu reads falls back",
			set:  map[string]string{"AZURESUBSCRIPTION_SERVICE_CONNECTION_ID": "connection"},
		},
		{
			name:  "request url without a service connection falls back",
			unset: []string{"ARM_ADO_PIPELINE_SERVICE_CONNECTION_ID", "ARM_OIDC_AZURE_SERVICE_CONNECTION_ID"},
		},
		{
			name:  "service connection without a request token falls back",
			unset: []string{"ARM_OIDC_REQUEST_TOKEN", "SYSTEM_ACCESSTOKEN"},
		},
		{
			name:  "service connection without OIDC falls back",
			unset: []string{"ARM_USE_OIDC"},
		},
		{
			name:  "Azure SDK client id without its ARM counterpart falls back",
			unset: []string{"ARM_CLIENT_ID"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := maps.Clone(adoEnv)
			maps.Copy(env, tt.set)

			for _, key := range tt.unset {
				delete(env, key)
			}

			config := backend.Config{
				"container_name":       "tfstate",
				"key":                  "service.tfstate",
				"resource_group_name":  "state",
				"storage_account_name": "stateaccount",
				"use_azuread_auth":     true,
			}
			maps.Copy(config, tt.config)

			remoteState := &remotestate.RemoteState{Config: &remotestate.Config{
				BackendName:   "azurerm",
				BackendConfig: config,
			}}

			v := (&venv.Venv{}).WithEnv(env)

			assert.Equal(t, tt.want, azureDirectStateReadSupported(v, &ParsingContext{}, remoteState))
		})
	}
}
