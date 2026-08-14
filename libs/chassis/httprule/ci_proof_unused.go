package httprule

// TEMPORARY, and this branch is never merged. The companion to ci_proof.go,
// which carries a formatting violation. This one is correctly formatted and
// carries a real linter violation instead: ciProofUnused is unexported and
// referenced by nothing, so `unused` should object.
//
// Two files rather than two violations in one, because golangci-lint reported
// only the first when they shared a file, and the point of this branch is to
// see both checks fire rather than to assume they would.
func ciProofUnused() string {
	return "this function is never called"
}
