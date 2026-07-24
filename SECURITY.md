# Security policy

## Supported deployment posture

LocalBilibili is intended for a trusted local network. Put it behind a reverse proxy with TLS and access controls before exposing it outside that boundary. Keep its SQLite database, media directories, configuration file, and Bilibili Cookie private.

## Reporting a vulnerability

Do not disclose suspected vulnerabilities in a public issue. Before a public repository exists, report through a private channel agreed with the project owner. When the GitHub repository is created, enable private vulnerability reporting and use its Security tab for sensitive reports.

## Secret handling

Never commit Cookies, access tokens, passwords, private keys, session files, database files, downloaded media, raw upstream responses, or machine-specific paths. If a secret is exposed, revoke or rotate it immediately and remove it from all reachable history before publishing.
