package content

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// ReadVerifiedRange verifies the complete body while retaining only the requested
// range. Returned bytes and digest evidence come from the same read, so replacing
// a file between verification and a second range read cannot evade validation.
func (s *Store) ReadVerifiedRange(body Body, offset int64, length int) ([]byte, error) {
	if !validDigest(body.Digest) || body.Size < 0 || body.Size > 64<<20 || offset < 0 || offset > body.Size || length < 1 || length > MaxReadSize {
		return nil, errors.New("invalid content body or range")
	}
	f, err := openRegular(s.path(body.Digest))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() != body.Size {
		return nil, errContentMismatch
	}
	h := sha256.New()
	if _, err := io.CopyN(h, f, offset); err != nil {
		return nil, err
	}
	if remaining := body.Size - offset; int64(length) > remaining {
		length = int(remaining)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, err
	}
	_, _ = h.Write(data)
	remaining := body.Size - offset - int64(length)
	n, err := io.Copy(h, io.LimitReader(f, remaining+1))
	if err != nil {
		return nil, err
	}
	if n != remaining || hex.EncodeToString(h.Sum(nil)) != body.Digest {
		return nil, errContentMismatch
	}
	return data, nil
}
