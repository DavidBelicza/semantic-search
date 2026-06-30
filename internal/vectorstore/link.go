package vectorstore

/*
#cgo darwin,arm64 LDFLAGS: ${SRCDIR}/../../native/lib/darwin_arm64/liblancedb_go.a -framework Security -framework CoreFoundation
#cgo darwin,amd64 LDFLAGS: ${SRCDIR}/../../native/lib/darwin_amd64/liblancedb_go.a -framework Security -framework CoreFoundation
#cgo linux,arm64 LDFLAGS: ${SRCDIR}/../../native/lib/linux_arm64/liblancedb_go.a -lm -ldl -lpthread
#cgo linux,amd64 LDFLAGS: ${SRCDIR}/../../native/lib/linux_amd64/liblancedb_go.a -lm -ldl -lpthread
#cgo windows,amd64 LDFLAGS: ${SRCDIR}/../../native/lib/windows_amd64/liblancedb_go.a
*/
import "C"
