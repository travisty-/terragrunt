## Description

Fixes #000.

Lets Terragrunt read azurerm dependency outputs straight from state when the backend authenticates through an Azure DevOps service connection.

- `azurehelper` requests the federated token for a service connection the way the backend's Azure DevOps authorizer does: a POST that adds `api-version=7.1`, `serviceConnectionId` and `audience` when the URL lacks them, read from `oidcToken`. It reads the same variables as Terraform's backend, and the service connection flow comes before the GitHub flow, as in both native backends.
- The direct-read gate treats that combination as supported when `use_oidc` is on, a service connection id, request URL and request token are set, and no static token or token file is set. In that case the request variables, `SYSTEM_OIDCREQUESTURI`, `SYSTEM_ACCESSTOKEN` and `AZURE_FEDERATED_TOKEN_FILE` no longer force a fallback, `AZURE_CLIENT_ID` and `AZURE_TENANT_ID` don't either when their `ARM_*` values are set, and OIDC counts as complete without a token file.
- Everything else falls back as before, including a service connection with a token file, `ARM_USE_AZUREAD_AUTH` and `AZURESUBSCRIPTION_SERVICE_CONNECTION_ID`.
- The Azure authentication docs describe the Azure DevOps setup.

Tested with `go test ./pkg/config/... ./internal/azurehelper/... ./internal/remotestate/...` and `golangci-lint run` on those packages.
The direct read itself was verified in a real Azure DevOps pipeline with the `extra_arguments` workaround from the issue, not with this branch.

## TODOs

### These must be true

- [ ] Read the [Contribution Guidelines](https://docs.terragrunt.com/community/contributing/)
- [ ] Run relevant [tests](https://docs.terragrunt.com/community/contributing/#running-tests).
- [ ] Run relevant [lints](https://docs.terragrunt.com/community/contributing/#linting).
- [ ] This change is backwards compatible (upgrading will not break existing users).
- [ ] This change is forwards compatible (downgrading will not break upgraded users).

### These might be true

- [ ] I authored this code entirely myself.
- [ ] I would like this PR taken over by a maintainer if changes are necessary (without expecting this).
- [ ] I am introducing or updating a dependency and confirm it has a [compatible open source license](https://github.com/gruntwork-io/terragrunt/blob/main/.licensei.toml).
- [ ] I am deprecating functionality. I am doing so using a [strict control](https://docs.terragrunt.com/reference/strict-controls/).
- [ ] I am introducing experimental functionality. I am doing so using an [experiment](https://docs.terragrunt.com/reference/experiments/).
