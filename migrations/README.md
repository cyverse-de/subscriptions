# Schema migrations

These are the schema migrations for the `qms` database, which this service
shares with the `qms` service.

**Until the QMS merge completes, `cyverse/qms` is the canonical copy and this
one is a mirror.** QMS still runs these migrations on startup, so a new
migration has to be added there and copied here, not the other way around. Once
QMS is retired the copy in that repo goes away and this becomes the only one.

The files are embedded (see `embed.go`) so that running them doesn't depend on
the process's working directory.
