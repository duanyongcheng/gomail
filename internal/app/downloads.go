package app

import (
	"context"
	"errors"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
)

type DownloadFailuresRequest struct {
	AccountID string `json:"accountId"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
}

func (s *SyncService) DownloadFailures(ctx context.Context, request DownloadFailuresRequest) (domain.DownloadFailuresPage, error) {
	if request.Limit == 0 {
		request.Limit = 20
	}
	if request.Limit < 1 || request.Limit > 100 || request.Offset < 0 {
		return domain.DownloadFailuresPage{}, errors.New("invalid failure list pagination")
	}
	return s.Runtime.DB.DownloadFailures(ctx, request.AccountID, request.Limit, request.Offset)
}

// Mark eligibility before triggering; if a shared pass has already completed
// its final queue check, arrange another pass before returning to manual mode.
func (s *SyncService) RetryDownloads(ctx context.Context, request AccountRequest) (Empty, error) {
	r := s.Runtime
	accounts, err := r.DB.MessageFailureAccounts(ctx, request.AccountID)
	if err != nil {
		return Empty{}, err
	}
	var failures []error
	for _, account := range accounts {
		if _, err := r.provider(account); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := r.DB.QueueMessageFailures(ctx, account); err != nil {
			failures = append(failures, err)
			continue
		}
		result, err := r.Scheduler.Trigger(ctx, account, "manual")
		if err == nil && !result.Limited {
			_, due, countErr := r.DB.MessageFailureCounts(ctx, account, time.Now().Unix())
			if countErr != nil {
				err = countErr
			} else if due > 0 {
				_, err = r.Scheduler.Trigger(ctx, account, "manual")
			}
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return Empty{}, errors.Join(failures...)
}
