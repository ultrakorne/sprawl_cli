# Health — Design

## Overview

`sprawl health` resolves credentials (token + agent secret) and calls `GET /api/v1/health`. Success renders `{"status":"ok"}`; failure renders the standard error envelope. Exit 1 on any failure.

## Behaviour

- **Missing agent secret fails before the HTTP call** with a clear pre-flight error, catching misconfiguration without burning a server round-trip.
- Structured output honours `--format` / `SPRAWL_OUTPUT` like every other command. Default is TOON.
- Text fallback for success is `status: ok`; for errors, the message goes to stderr.
