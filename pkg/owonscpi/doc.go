// Package owonscpi compiles and interprets the OWON HDS command dialect.
// Its pure functions own command tokens, supported setting matrices and payload interpretation,
// not I/O, clocks, connection ownership or operation sequencing.
//
// Example: CompileChannel produces an ordered write plan and explicit observed-probe preflight data.
package owonscpi
