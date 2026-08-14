package httprule

import (
	"fmt"
	"os"
)

// TEMPORARY, and this branch is never merged. It proves the ENT-214 lint and
// format steps can actually fail, the same way the file next door does for
// Prettier.
//
// Two deliberate violations, chosen so the package still builds and still
// passes its tests: the layout is not gofmt's, and CIProofWrite is exported
// and referenced by nothing, so only the formatter should object. If the Go
// job is green with this file present, the formatting check is not running.
func   CIProofWrite( ) {
	fmt.Fprintln( os.Stderr,   "ci proof" )
}
