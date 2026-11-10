# Security policy

## Reporting a problem

Please report security problems privately, not in a public issue.

- Contact: **[security contact to be set by the maintainer]**
- Include: the version or image tag, how to reproduce it, and what you can
  access because of it.

You will get an acknowledgement within a few days. Please give the maintainer
time to fix the problem before you publish details.

## Supported versions

Fixes go to the latest version. Older versions are not patched.

## What the app protects

- Each household's data is kept apart. A test checks that every route refuses
  another household's IDs.
- AI tokens and access tokens are never returned by the API after they are
  saved. Access tokens are stored as hashes; AI tokens are encrypted on disk
  with the instance key (`secret.key` or `INVENTORY_SECRET_KEY`).
- Keep `/data`, including `secret.key`, private. Anyone with it can read the
  database and the stored AI tokens.
