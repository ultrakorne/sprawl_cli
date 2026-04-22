# Output Formats — Design

## Overview

Every `/api/v1/*`-wrapping subcommand honours a uniform `--format=text|json|toon` persistent flag. `toon` is the default (compact, LLM-friendly — typically 30–60 % fewer tokens than JSON); `json` is the server's wire shape unchanged; `text` is a human-readable fallback (tabwriter tables for lists, multi-line details for single records). `SPRAWL_OUTPUT` sets a session default without repeating the flag. Login is interactive and stays plain text regardless — agents can't approve in a browser anyway.

## Error envelope

When a command fails in `json` or `toon` mode, the error is rendered as a structured envelope rather than free text:

```json
{"status": "error", "error": "<message>", "http_status": 403}
```

`http_status` is omitted for errors that never made it to the wire (e.g. missing agent secret). In `text` mode, errors go to stderr and nothing is written to stdout.

## Precedence

Format resolution: `--format` flag → `SPRAWL_OUTPUT` env → `toon` default. Whitespace and case are normalised; invalid values return an error (no silent fallback).

## Design Decisions

- **TOON is default, not JSON**: agents consume it more compactly while staying lossless. JSON is one flag away when piping into `jq`.
- **Error envelope uses `error` (not `code`)**: mirrors the server's error field so agents parse one shape regardless of origin.
- **Plain-text errors go to stderr only**: avoids corrupting a stdout pipe when `text` mode is used in scripts that check exit status.
