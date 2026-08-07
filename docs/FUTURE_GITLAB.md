# Future GitLab support

GitLab support is intentionally absent from the current release. All legacy GitLab settings, flows, grants and encrypted credentials are removed by schema v5.

GitLab may be reconsidered only when it can provide project-scoped, short-lived and promptly revocable credentials whose permissions cannot leak a user's shared OAuth authority. A future adapter must preserve the same execution-derived repository boundary and secret-handling guarantees as the GitHub installation-token broker. Until then, the product, API and UI support GitHub only.
