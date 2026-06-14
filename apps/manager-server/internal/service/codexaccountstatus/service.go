package codexaccountstatus

import (
	"context"
	"errors"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

var ErrNoCodexInspectionRun = errors.New("codex inspection run not found")

type Service struct {
	store *store.Store
}

func New(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) Latest(ctx context.Context) (model.CodexAccountStatusResponse, error) {
	runs, err := s.store.ListCodexInspectionRuns(ctx, 1)
	if err != nil {
		return model.CodexAccountStatusResponse{}, err
	}
	if len(runs) == 0 {
		return model.CodexAccountStatusResponse{}, ErrNoCodexInspectionRun
	}
	items, err := s.store.ListCodexAccountStatusItems(ctx, runs[0].ID)
	if err != nil {
		return model.CodexAccountStatusResponse{}, err
	}
	return model.CodexAccountStatusResponse{Run: runs[0], Items: items}, nil
}
