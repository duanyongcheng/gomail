package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/duanyongcheng/pebble-go/internal/app"
	"github.com/egoist/mygo"
)

// FileService owns only native directory/dialog adaptation. Attachment lookup,
// constrained reads and copying remain in the framework-independent core.
type FileService struct {
	Runtime    *app.Runtime
	App        *AppService
	SaveDialog func(mygo.SaveDialogOptions) (string, error)
}

func (s *FileService) Downloads(app.Empty) (string, error) {
	directory, err := mygo.App.Path(mygo.PathDownloads)
	if err != nil || !filepath.IsAbs(directory) {
		return "", errors.New("cannot locate Downloads directory")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", errors.New("cannot create Downloads directory")
	}
	return directory, nil
}

// SaveAttachment returns null on dialog cancellation; it never opens files in
// their associated app. Even a dialog-confirmed existing target is not replaced.
func (s *FileService) SaveAttachment(ctx context.Context, request app.AttachmentRequest) (*string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.Runtime.Context, cancel)
	defer stop()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	core := &app.AttachmentService{Runtime: s.Runtime}
	available, err := core.Path(ctx, request)
	if err != nil {
		return nil, err
	}
	if available == nil {
		return nil, errors.New("attachment file is unavailable; sync the account to recover it")
	}
	a, _, err := s.Runtime.DB.Attachment(ctx, request.AttachmentID)
	if err != nil {
		return nil, errors.New("attachment not found")
	}
	directory, err := s.Downloads(app.Empty{})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	options := mygo.SaveDialogOptions{DefaultPath: filepath.Join(directory, app.SuggestedAttachmentFilename(a.Filename)), CreateDirectories: true}
	if s.App != nil {
		options.Parent = s.App.Window.Load()
	}
	save := s.SaveDialog
	if save == nil {
		save = mygo.Dialog.Save
	}
	path, err := save(options)
	if err != nil {
		return nil, errors.New("cannot show attachment save dialog")
	}
	if path == "" {
		return nil, nil
	}
	actual, err := core.Download(ctx, app.DownloadAttachmentRequest{AttachmentID: a.ID, SaveTo: path})
	if err != nil {
		return nil, err
	}
	return &actual, nil
}
