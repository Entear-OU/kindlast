// Package server wires the Connect handlers that core-api serves.
//
// At this point it holds only the service registry: the list of proto
// services this binary exposes. The handlers themselves, and the interceptor
// chain that fronts them, arrive with ENT-195 and ENT-196.
package server

import (
	"google.golang.org/protobuf/reflect/protoreflect"

	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Services returns every proto service core-api exposes.
//
// It exists so the scope-declaration test has one place to enumerate, rather
// than a list in a test file that someone forgets to extend. Adding a service
// here without declaring scopes on its methods fails that test, which is the
// whole point: the check must be impossible to skip by forgetting.
func Services() []protoreflect.ServiceDescriptor {
	files := []protoreflect.FileDescriptor{
		corev1.File_kindlast_core_v1_session_proto,
		corev1.File_kindlast_core_v1_org_proto,
		// Listed from the moment the contract exists rather than when its
		// handlers do. The scope-declaration test then covers FindingsService
		// and DashboardService immediately, so an RPC cannot reach main
		// undeclared during the window where the proto has landed and the
		// service has not.
		corev1.File_kindlast_core_v1_findings_proto,
		// RecordsService, listed under the same rule while it is contract only
		// (ENT-200). Its handlers are not registered on the mux yet, so nothing
		// here is reachable; what this buys is that the six RPCs are scope
		// checked now, and the day a handler is added it cannot arrive
		// undeclared.
		corev1.File_kindlast_core_v1_records_proto,
		// The internal surface is enumerated here too, so the scope-declaration
		// test covers it. An internal RPC is the last place an undeclared scope
		// should be able to hide: these carry `internal:*`, which is the
		// vocabulary that can act across organisations.
		platformv1.File_kindlast_platform_v1_sweep_proto,
	}

	var services []protoreflect.ServiceDescriptor
	for _, file := range files {
		descriptors := file.Services()
		for i := 0; i < descriptors.Len(); i++ {
			services = append(services, descriptors.Get(i))
		}
	}
	return services
}
