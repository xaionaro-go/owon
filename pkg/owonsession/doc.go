// Package owonsession owns exclusive access and recovery for one OWON connection.
// Transactions retain admission across multiple protocol commands. Ambiguous I/O
// failures quarantine the connection until identity-validated recovery; commands
// are never replayed. Concrete I/O belongs to the supplied Backend.
//
// Example: New constructs one shared session for concurrent instrument operations.
package owonsession
