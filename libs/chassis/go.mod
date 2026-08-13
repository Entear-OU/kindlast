// INFRASTRUCTURE ONLY. No business types (core-api-surface §21.4, §21.5).
//
// The test for whether something belongs here: could it be open-sourced
// without mentioning compliance, findings or GDPR? If not, it belongs to one
// service. A shared domain package would produce a distributed monolith, two
// binaries that cannot change independently because they compile against the
// same business rules.
module github.com/Entear-OU/kindlast/libs/chassis

go 1.25.0

require google.golang.org/protobuf v1.36.12

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/redis/go-redis/v9 v9.22.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260810153831-ec0a7760b754 // indirect
)
