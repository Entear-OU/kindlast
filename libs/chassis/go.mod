// INFRASTRUCTURE ONLY. No business types (core-api-surface §21.4, §21.5).
//
// The test for whether something belongs here: could it be open-sourced
// without mentioning compliance, findings or GDPR? If not, it belongs to one
// service. A shared domain package would produce a distributed monolith, two
// binaries that cannot change independently because they compile against the
// same business rules.
module github.com/Entear-OU/kindlast/libs/chassis

go 1.24

require google.golang.org/protobuf v1.36.12
