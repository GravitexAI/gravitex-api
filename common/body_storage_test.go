package common

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// ReaderOnly must keep the seek capability of a seekable body (BodyStorage) so
// that http.Request.GetBody can rewind and retry after a lost connection, while
// still hiding io.Closer from the HTTP transport (which would otherwise close the
// underlying storage prematurely).
func TestReaderOnlyPreservesSeekForRewind(t *testing.T) {
	data := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`)
	storage, err := CreateBodyStorage(data)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })

	r := ReaderOnly(storage)

	// io.Closer must stay hidden: the transport must not be able to close the
	// underlying BodyStorage by type-asserting the body.
	_, isCloser := r.(io.Closer)
	require.False(t, isCloser, "ReaderOnly must hide io.Closer")

	// Seek must be preserved so the body is rewindable for retries.
	seeker, isSeeker := r.(io.Seeker)
	require.True(t, isSeeker, "ReaderOnly must preserve io.Seeker for seekable bodies")

	first, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, data, first)

	// Rewind and read again — the full body must come back byte-for-byte.
	_, err = seeker.Seek(0, io.SeekStart)
	require.NoError(t, err)
	second, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, data, second)
}

// A non-seekable body must not gain a phantom Seek capability, so callers can
// reliably detect that it cannot be rewound.
func TestReaderOnlyNonSeekableStaysNonSeekable(t *testing.T) {
	// *bytes.Buffer is an io.Reader but not an io.Seeker.
	r := ReaderOnly(bytes.NewBufferString("plain body"))

	_, isSeeker := r.(io.Seeker)
	require.False(t, isSeeker, "non-seekable body must not expose io.Seeker")

	got, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "plain body", string(got))
}
