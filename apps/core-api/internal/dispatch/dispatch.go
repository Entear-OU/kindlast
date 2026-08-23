// Package dispatch turns pending notifications into email, on a timer inside
// core-api.
//
// # WHAT IS LEFT HERE, AND WHAT WENT WHERE
//
// This package used to hold two loops. The transactional outbox drain (ENT-219),
// which delivered invitation mail, and its retention pass (ENT-242) moved to
// Temporal in ENT-256 part three: a Schedule on `workers` relays each pending
// row into its own workflow, and the activity calls DeliveryService on the
// internal surface, which claims, sends and records inside core-api exactly as
// the loop did. What survived the move unaltered is the rule the loop existed
// to serve: the message is written in the transaction that makes its fact true,
// and delivery is a separate concern that may fail, retry and lag. What moved
// is the timer, the retry policy and the record of attempts, which are now the
// engine's, where an operator can see them.
//
// The doorbell path (ENT-209) is still here, on the same ticker shape, and it
// goes the same way next: one workflow per notification, with quiet hours as a
// durable timer rather than a dropped message. Until then this package is that
// one loop and the two constants it shares with anything else still ticking.
package dispatch

import "time"

// Defaults for a loop that polls a table on a timer.
//
// Ten seconds is well inside "within a minute or so", which is what a person
// clicking a button expects of the mail it produces, and costs one trivial
// indexed query per tick against a partial index sized to the backlog, which on
// an idle deployment is empty.
//
// The batch bounds how long one tick can run: each message is its own
// transaction holding one row lock across one SMTP conversation, so twenty is
// twenty short locks rather than one long one.
const (
	DefaultInterval = 10 * time.Second
	DefaultBatch    = 20
)
