// Package server wires the Connect handlers that core-api serves.
//
// At this point it holds only the service registry: the list of proto
// services this binary exposes. The handlers themselves, and the interceptor
// chain that fronts them, arrive with ENT-195 and ENT-196.
package server

import (
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
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
