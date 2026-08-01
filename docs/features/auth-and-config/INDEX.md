# Authentication & Configuration

Device-flow login (RFC 8628), credential resolution per request (bearer token plus at least one narrowing factor — a project key and/or an agent secret), two-binary build pattern with ldflag-injected URLs, and XDG-aware config storage. Underpins every authenticated CLI command.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | UX, security model, credential rules, two-binary rationale |
| [TECHNICAL.md](TECHNICAL.md) | Source files, resolution order, wire invariants |
| [FLOW.mermaid](FLOW.mermaid) | Device-flow sequence (CLI ↔ server ↔ browser) |
