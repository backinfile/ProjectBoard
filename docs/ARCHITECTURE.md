# Architecture and trust boundaries

`Browser → Web/API → domain services → SQLite` owns identity, authorization, stage transitions and audit. `Runner → Runner API` owns local code execution. `Web/API → Broker → Git provider` is the only credential path.

The Broker exposes exactly two issuance decisions internally: observer credentials derived from a bound repository, and Runner credentials derived from run + assignment + project grant + active lease + repository binding. Callers cannot request arbitrary repository IDs or permission levels. Provider secrets remain in Broker-only files/environment; issued plaintext tokens remain in Broker memory and the Runner credential adapter only.

Transactions never perform network, Git, Codex or long-running commands. Version, stage, lease termination, immutable attempt creation, timeline insertion and audit insertion are coordinated in short SQLite transactions. External work is validated before a second transactional submission.

All Markdown is untrusted. Raw HTML and dangerous schemes are removed, attachments require project authorization and download with `nosniff`, a restrictive CSP and private/no-store caching. Audit payloads pass through the shared recursive redactor.
