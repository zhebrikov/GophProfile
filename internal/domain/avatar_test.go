package domain_test

import (
	"testing"

	"github.com/practicum/gophprofile/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestThumbnailsRoundTrip(t *testing.T) {
	a := &domain.Avatar{}
	thumbs := []domain.ThumbnailInfo{
		{Size: "100x100", Key: "thumbnails/a/100x100.jpg"},
		{Size: "300x300", Key: "thumbnails/a/300x300.jpg"},
	}
	require.NoError(t, a.SetThumbnails(thumbs))
	got, err := a.Thumbnails()
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "100x100", got[0].Size)
}

func TestThumbnailsEmpty(t *testing.T) {
	a := &domain.Avatar{}
	got, err := a.Thumbnails()
	require.NoError(t, err)
	require.Nil(t, got)
}
