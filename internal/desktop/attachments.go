package desktop

import (
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/privacy"
	"github.com/duanyongcheng/pebble-go/internal/store"
	"github.com/egoist/mygo"
)

const attachmentScheme = "pebble-attachment"

// RegisterAttachmentProtocol exposes only attachment IDs signed into renderer
// URLs. It never accepts a filesystem path or a message/account chosen solely
// by a browser request.
func RegisterAttachmentProtocol(db *store.DB) error {
	return mygo.Protocol.Handle(attachmentScheme, AttachmentHandler(db))
}

func AttachmentHandler(db *store.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		accountID, messageID, attachmentID, ok := privacy.DecodeAttachmentURL(path.Clean(r.URL.Path))
		if !ok {
			http.NotFound(w, r)
			return
		}
		handle, file, err := db.OpenAttachment(r.Context(), accountID, messageID, attachmentID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer handle.Close()
		info, err := handle.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", file.MIMEType)
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "attachment", info.ModTime().Round(time.Second), handle)
	})
}
