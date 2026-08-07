// Package migrations embeds the QMS schema migrations so that callers can run
// them without depending on the process's working directory.
package migrations

import "embed"

// FS holds the golang-migrate migration files. Use it with the iofs source
// driver; the file source scanner ignores this file because its name doesn't
// match the migration naming pattern.
//
//go:embed *.sql
var FS embed.FS
