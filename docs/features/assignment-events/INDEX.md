# Assignment Events

How an agent learns that an item was assigned to it, or taken off it, without scanning boards. `events watch` follows the calling agent key's assignment feed as a JSON-lines stream with a cursor per line, `events show <id>` reads one event back, and `queue --assignee me` is the inventory a listener reconciles against. The server is the source of truth for which events a key sees; the CLI only follows, retries and prints.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | The stream contract, cursor ownership, reconcile points, the inventory |
| [TECHNICAL.md](TECHNICAL.md) | Feed transport, the retry/fatal split, flushing and shutdown, where things live |
