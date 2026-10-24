## Describe the enhancement

We run Terragrunt 1.2.1 in Azure DevOps with one workload identity federation service connection per environment, and our `--auth-provider-cmd` gives each unit its own identity.
Since 1.2.0 reads dependency outputs straight from state, I was hoping to drop the `terraform output` fallback, but every azurerm read falls back for us.

It looks like the Azure DevOps service connection flow is the reason.
The variables the `azurerm` backend reads for it are all in `azureUnsupportedEnvKeys`:

https://github.com/gruntwork-io/terragrunt/blob/v1.2.1/pkg/config/dependency_state_azurerm.go#L94

and OIDC only counts as complete with a token file:

https://github.com/gruntwork-io/terragrunt/blob/v1.2.1/pkg/config/dependency_state_azurerm.go#L512

I think the gate is right to reject them today, because `azurehelper` doesn't request the token the way the backend does.
Its `SYSTEM_OIDCREQUESTURI` branch sends a GET with no `serviceConnectionId`:

https://github.com/gruntwork-io/terragrunt/blob/v1.2.1/internal/azurehelper/oidc.go#L53

while the backend's Azure DevOps authorizer (go-azure-sdk, which Terraform's backend uses) sends a POST, adds `api-version=7.1`, `serviceConnectionId` and `audience` when the URL doesn't have them, and reads `oidcToken`:

https://github.com/hashicorp/go-azure-sdk/blob/main/sdk/auth/ado_pipeline_oidc_authorizer.go

So I'd like to propose making `azurehelper` match that flow, then letting the gate through when every implementation would use the service connection.

## Additional context

- [x] Changes required.
  - `azurehelper`: when `ARM_ADO_PIPELINE_SERVICE_CONNECTION_ID` or `ARM_OIDC_AZURE_SERVICE_CONNECTION_ID` is set along with a request URL, request the assertion the way go-azure-sdk does, from the same variables Terraform's backend reads (`ARM_OIDC_REQUEST_URL`, then `ACTIONS_ID_TOKEN_REQUEST_URL`, then `SYSTEM_OIDCREQUESTURI`, with the matching request tokens), ahead of the GitHub flow, which is the order both native backends use.
  - The gate: when `use_oidc` is on, a service connection id, a request URL and a request token are set, and neither `ARM_OIDC_TOKEN` nor a token file is set, stop rejecting the request variables, `SYSTEM_OIDCREQUESTURI` and `SYSTEM_ACCESSTOKEN`, and count OIDC as complete without a token file.
  - In that mode, also allow `AZURE_FEDERATED_TOKEN_FILE`, which none of them read once the service connection wins, and `AZURE_CLIENT_ID`/`AZURE_TENANT_ID` when the `ARM_*` value is set, since every implementation reads `ARM_*` first.
    We set the `AZURE_*` variables for providers that sign in through `DefaultAzureCredential` (cyrilgdn/postgresql and betr-io/mssql).
- [x] Implications of the feature.
  - A token file or `ARM_OIDC_TOKEN` alongside a service connection still falls back: Terraform's backend prefers those to the service connection, and OpenTofu's and `azurehelper` don't, so they could disagree.
  - `ARM_USE_AZUREAD_AUTH` and `AZURESUBSCRIPTION_SERVICE_CONNECTION_ID` still fall back, since only `azurehelper` reads the first and only OpenTofu reads the second.
  - One thing I'm not sure about: OpenTofu's backend takes the URL from `SYSTEM_OIDCREQUESTURI` and ignores `ARM_OIDC_REQUEST_URL`, while Terraform's uses `ARM_OIDC_REQUEST_URL` first.
    I don't think that changes the identity, because the client id decides it and an assertion for a different connection fails the exchange instead of signing in as someone else, but let me know if you'd rather the gate require the two to agree.
- [x] Alternatives considered.
  - For now we blank the rejected variables and point `ARM_OIDC_TOKEN_FILE_PATH` at a token file in an `extra_arguments` block scoped to `output`, which works because those env vars are applied right before the gate:

    https://github.com/gruntwork-io/terragrunt/blob/v1.2.1/pkg/config/dependency.go#L2041

    That puts our auth details in `root.hcl` and depends on a token file being fresh when the read happens, which is why I'd rather Terragrunt support the flow directly.
  - Putting `ARM_OIDC_TOKEN_FILE_PATH` in the whole run environment isn't an option for us: azurerm and azuread read the file once when the provider is configured and prefer it to the service connection, so a long apply can't renew its token.
- [x] Level of effort.
  - Small. I have a branch with the change and tests (linked below).

For what it's worth, the workaround already shows the difference for us: a dependency read directly from state took 0.6 s, and one that fell back took 9.2 s (`terraform init` plus `terraform output`).
It also gets rid of the `terraform init` that a fallback runs in the dependency's own directory, which has collided with that unit running in the same `run --all`.

## PoC (Proof of Concept)

- [x] [PoC](https://github.com/travisty-/terragrunt/tree/feat/azurerm-ado-direct-reads)

## RFC Not Needed

- [ ] I have evaluated the complexity of this enhancement, and I believe it does not require an RFC.
