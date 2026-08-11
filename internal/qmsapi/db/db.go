package db

// QMS opened its own database connection here. The subscriptions service owns
// the connection now and hands it to the /v1 handlers, so the connection setup
// that used to live in this file is gone.

// uniqueIDs returns ids with duplicates removed, preserving the order of first occurrence. The batch loaders that build
// "WHERE id IN (...)" lists call this before querying: a page of rows routinely repeats the same foreign key (many
// subscriptions on one plan, many quota rows on one of two resource types), and in prepared mode each repeat is a bind
// parameter, not free text — PostgreSQL caps a statement at 65535 of them.
func uniqueIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	deduped := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	return deduped
}
