package storage_test

import (
	"testing"

	"github.com/practicum/gophprofile/pkg/storage"
	"github.com/stretchr/testify/require"
)

func TestPublicAndObjectURL(t *testing.T) {
	s, err := storage.NewS3Storage("localhost:9000", "k", "s", "avatars", false, "http://localhost:9000/")
	require.NoError(t, err)
	require.Equal(t, "http://localhost:9000/avatars/originals/a.jpg", s.PublicURL("originals/a.jpg"))
	require.Equal(t, "/api/v1/files/originals/a.jpg", s.ObjectURL("originals/a.jpg"))
}
