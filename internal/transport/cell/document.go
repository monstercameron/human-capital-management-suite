package cell

import (
	"google.golang.org/grpc"

	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

// RegisterDocument adds the same canonical service to direct and browser
// gRPC surfaces. With no configured document database its handler reports
// UNAVAILABLE without exposing a second data path.
func RegisterDocument(srv *grpc.Server, service transportdocument.Service, cursorKey []byte) {
	transportdocument.Register(srv, service, cursorKey)
}
