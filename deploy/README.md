# Deployment

Run the Web/API and Git Credential Broker as separate operating-system identities. Only the Broker receives provider private-key/client-secret environment variables; expose it on loopback or a private service network. Protect GitHub `main`, `develop`, `release/*` and tags with Rulesets and do not add the ProjectBoard App to bypass lists.

Linux uses `projectboard-runner.service`. On Windows, run `projectboard-runner poll --watch` as the current user and use the tray launcher in `src/runner/tray.ps1`; it never requests elevation.
