package intelligence

import (
	"context"
	"errors"
	"unicode/utf8"
)

const (
	// MaxArtifactChunkBytes bounds one resource response. The limit is applied
	// to UTF-8 bytes, while never splitting a code point.
	MaxArtifactChunkBytes int64 = 64 << 10
)

var (
	// ErrArtifactOffset means a resource cursor is outside a UTF-8 boundary.
	ErrArtifactOffset = errors.New("artifact offset out of range")
	// ErrArtifactLimit means a requested resource chunk is empty or too large.
	ErrArtifactLimit = errors.New("invalid artifact chunk limit")
)

// ArtifactChunk is a deterministic, snapshot-bound slice of an artifact.
// Offset and TotalBytes are byte offsets, not rune or model-token counts.
type ArtifactChunk struct {
	ID         string `json:"id"`
	SnapshotID string `json:"snapshot_id"`
	Offset     int64  `json:"offset"`
	TotalBytes int64  `json:"total_bytes"`
	Text       string `json:"text"`
	NextCursor string `json:"next_cursor,omitempty"`
	Complete   bool   `json:"complete"`
}

// ReadChunk reads a bounded UTF-8-safe portion of an artifact. The artifact
// is content-addressed and its persisted snapshot binding is revalidated before
// bytes are exposed. A cursor determines the offset when supplied.
func (s *ArtifactStore) ReadChunk(ctx context.Context, id, cursor string, offset, limit int64) (ArtifactChunk, error) {
	if err := contextError(ctx); err != nil {
		return ArtifactChunk{}, err
	}
	if !validID(id) {
		return ArtifactChunk{}, ErrArtifactNotFound
	}
	if limit <= 0 || limit > MaxArtifactChunkBytes {
		return ArtifactChunk{}, ErrArtifactLimit
	}
	if cursor != "" {
		c, err := DecodeArtifactCursor(cursor, id)
		if err != nil {
			return ArtifactChunk{}, err
		}
		offset = c.Offset
	}
	a, err := s.getVerified(id)
	if err != nil {
		return ArtifactChunk{}, err
	}
	if err := contextError(ctx); err != nil {
		return ArtifactChunk{}, err
	}
	total := int64(len(a.Payload))
	if offset < 0 || offset > total || (offset < total && !utf8.RuneStart(a.Payload[offset])) {
		return ArtifactChunk{}, ErrArtifactOffset
	}
	if offset == total {
		return ArtifactChunk{ID: a.ID, SnapshotID: a.SnapshotID, Offset: offset, TotalBytes: total, Complete: true}, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	// Do not return a partial code point. Since offset is validated at a rune
	// boundary, the final byte is safe whenever it reaches the payload end.
	for end > offset && end < total && !utf8.RuneStart(a.Payload[end]) {
		end--
	}
	if end == offset {
		return ArtifactChunk{}, ErrArtifactLimit
	}
	complete := end == total
	chunk := ArtifactChunk{ID: a.ID, SnapshotID: a.SnapshotID, Offset: offset, TotalBytes: total, Text: string(a.Payload[offset:end]), Complete: complete}
	if !utf8.ValidString(chunk.Text) {
		return ArtifactChunk{}, ErrArtifactCorrupt
	}
	if !complete {
		chunk.NextCursor, err = EncodeArtifactCursor(id, end)
		if err != nil {
			return ArtifactChunk{}, err
		}
	}
	if err := contextError(ctx); err != nil {
		return ArtifactChunk{}, err
	}
	return chunk, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
