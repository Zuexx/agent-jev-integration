# Notes on GitHub Actions (collected by the team)
- A job on a GitHub-hosted runner can run for at most 6 hours, then it is cancelled. Self-hosted runners allow up to 5 days per job.
- GitHub-hosted runners have no GPUs on our plan.
- Workflow logs and artifacts are kept for 90 days by default. An organization can raise this to at most 400 days for private repositories.
- Secrets can be read from Vault with the hashicorp/vault-action action and an OIDC role.
