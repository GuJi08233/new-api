package common

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBodyStorageReplayUsesIndependentReaders(t *testing.T) {
	original := GetDiskCacheConfig()
	t.Cleanup(func() { SetDiskCacheConfig(original) })
	SetDiskCacheConfig(DiskCacheConfig{Path: t.TempDir()})
	for _, disk := range []bool{false, true} {
		name := "memory"
		if disk {
			name = "disk"
		}
		t.Run(name, func(t *testing.T) {
			payload := []byte("complete request body")
			var storage BodyStorage = newMemoryStorage(payload)
			if disk {
				require.NoError(t, storage.Close())
				var err error
				storage, err = newDiskStorage(payload, "")
				require.NoError(t, err)
			}
			t.Cleanup(func() { require.NoError(t, storage.Close()) })
			_, err := storage.Seek(5, io.SeekStart)
			require.NoError(t, err)
			body, ok := ReaderOnly(storage).(ReplayableBody)
			require.True(t, ok)
			_, exposesClose := body.(io.Closer)
			assert.False(t, exposesClose)
			first, err := body.NewReader()
			require.NoError(t, err)
			second, err := body.NewReader()
			require.NoError(t, err)
			prefix := make([]byte, 3)
			_, err = io.ReadFull(first, prefix)
			require.NoError(t, err)
			secondBody, err := io.ReadAll(second)
			require.NoError(t, err)
			assert.Equal(t, payload, secondBody)
			require.NoError(t, second.Close())
			rest, err := io.ReadAll(first)
			require.NoError(t, err)
			assert.Equal(t, payload, append(prefix, rest...))
			require.NoError(t, first.Close())
			primary, err := io.ReadAll(storage)
			require.NoError(t, err)
			assert.Equal(t, payload[5:], primary)
			require.NoError(t, storage.Close())
			_, err = body.NewReader()
			assert.ErrorIs(t, err, ErrStorageClosed)
		})
	}
}
