# Security Policy

## Supported versions

Until Tally reaches its first public stable release, security fixes are applied to the latest code on `main` and to the newest published prerelease only.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability or privacy leak. Use GitHub’s private vulnerability-reporting flow on the Tally repository:

`Security` → `Advisories` → `Report a vulnerability`

Include the affected version or commit, reproduction steps, impact, and any proposed mitigation. The maintainers will acknowledge a complete report within five business days and will coordinate disclosure after a fix is available.

## Security boundaries

Tally is local-first. Its dashboard should remain bound to loopback unless an operator deliberately provides a secure network boundary. When `TALLY_API_TOKEN` is configured, all API access and browser-session issuance require authorization; browser mutations additionally require the session-specific CSRF token.

Tally stores activity metadata. Operators are responsible for configuring ignored applications and deciding whether URL paths may be persisted. Query strings, fragments, and URL credentials are removed before persistence.
