# Activity

`sprawl activity` — daily completion log against `GET /api/v1/activity_log`. Returns the calling agent's completed tasks and completed checklist items for a single day, scoped by the same permission cascade as `task list`. Default day is today in the user's timezone; `--date` and `--days-ago` select a different day (mutually exclusive).

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Behaviour, response shape, exit codes |
| [TECHNICAL.md](TECHNICAL.md) | Source file pointers |
