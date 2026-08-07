# Current implementation plan

ProjectBoard's active Agent architecture is the SSH public-key and direct Codex execution model described in `README.md`, `PRODUCT.md` and `docs/ARCHITECTURE.md`.

The supported execution surface is SSH MCP backed directly by the Agent Execution module. GitHub is the only repository provider. Runner and GitLab are future directions only; see `docs/FUTURE_RUNNER.md` and `docs/FUTURE_GITLAB.md`.
