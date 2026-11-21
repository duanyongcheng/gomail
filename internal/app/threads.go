package app

import (
	"context"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/store"
)

type ThreadsRequest struct {
	FolderID  string   `json:"folderId"`
	FolderIDs []string `json:"folderIds"`
	Limit     int      `json:"limit"`
	Offset    int      `json:"offset"`
}
type ThreadRequest struct {
	ThreadID string `json:"threadId"`
}
type ThreadService struct{ Runtime *Runtime }

func (s *ThreadService) List(ctx context.Context, request ThreadsRequest) ([]domain.ThreadSummary, error) {
	folders := request.FolderIDs
	if len(folders) == 0 && request.FolderID != "" {
		folders = []string{request.FolderID}
	}
	return s.Runtime.DB.Threads(ctx, store.ThreadsFilter{FolderIDs: folders, Limit: request.Limit, Offset: request.Offset})
}
func (s *ThreadService) Messages(ctx context.Context, request ThreadRequest) ([]domain.Message, error) {
	messages, err := s.Runtime.DB.ThreadMessages(ctx, request.ThreadID)
	if err != nil {
		return nil, err
	}
	for i := range messages {
		if p, err := s.Runtime.provider(messages[i].AccountID); err == nil {
			messages[i].Capabilities = p.Capabilities()
		}
	}
	return messages, nil
}
