package repository

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestUpstreamChannelRepositoryListFiltersByProvider(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewUpstreamChannelRepository(db)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM upstream_channels WHERE .*EXISTS .*upstream_platforms\.channel_id = upstream_channels\.id AND upstream_platforms\.provider = \$1`).
		WithArgs("openai").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery(`SELECT id, name, description, status, created_at, updated_at\s+FROM upstream_channels WHERE .*EXISTS .*upstream_platforms\.provider = \$1.*ORDER BY created_at desc, id DESC LIMIT \$2 OFFSET \$3`).
		WithArgs("openai", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "description", "status", "created_at", "updated_at"}))

	channels, pag, err := repo.List(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, "", "", "openai")

	require.NoError(t, err)
	require.Empty(t, channels)
	require.Equal(t, int64(0), pag.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}
