package rlm

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Images have their own aggregate limit. Normal frames remain small, and every
// chunk is correlated, ordered and checked before an image can be published.
func writeBlob(output io.Writer, limit int, id uint64, data []byte) error {
	digest := sha256.Sum256(data)
	if len(data) < 1 || len(data) > MaxCheckpointBytes {
		return errors.New("checkpoint image size limit")
	}
	if err := writeFrame(output, limit, frame{Type: "checkpoint_begin", ID: id, Bytes: len(data), SHA256: hex.EncodeToString(digest[:])}); err != nil {
		return err
	}
	return writeBlobChunks(output, limit, id, data)
}

func writeBlobChunks(output io.Writer, limit int, id uint64, data []byte) error {
	size := min(64<<10, (limit-256)/2)
	if size < 1 {
		return ErrFrameLimit
	}
	for offset := 0; offset < len(data); offset += size {
		if err := writeFrame(output, limit, frame{Type: "checkpoint_chunk", ID: id, Offset: offset, Data: data[offset:min(offset+size, len(data))]}); err != nil {
			return err
		}
	}
	return nil
}

func receiveBlob(begin frame, read func() (frame, error)) ([]byte, error) {
	if begin.Bytes < 1 || begin.Bytes > MaxCheckpointBytes || len(begin.SHA256) != 64 {
		return nil, errors.New("invalid checkpoint declaration")
	}
	data := make([]byte, 0, begin.Bytes)
	for len(data) < begin.Bytes {
		part, err := read()
		if err != nil {
			return nil, err
		}
		if part.Type != "checkpoint_chunk" || part.ID != begin.ID || part.Offset != len(data) || len(part.Data) == 0 || len(part.Data) > begin.Bytes-len(data) {
			return nil, errors.New("invalid checkpoint chunk")
		}
		data = append(data, part.Data...)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != begin.SHA256 {
		return nil, errors.New("checkpoint transfer integrity mismatch")
	}
	return data, nil
}

func readBlob(input *bufio.Reader, limit int, begin frame) ([]byte, error) {
	return receiveBlob(begin, func() (frame, error) { return readFrame(input, limit) })
}

func (kernel *Kernel) restoreCheckpointLocked(ctx context.Context) (*RestoreReport, bool, error) {
	loadCtx, cancel := context.WithTimeout(ctx, kernel.limits.Wall)
	defer cancel()
	checkpoint, err := kernel.checkpoints.Load(loadCtx)
	if err != nil {
		return nil, false, err
	}
	if checkpoint == nil {
		return nil, false, nil
	}
	if err := checkpoint.Validate(kernel.engine); err != nil {
		return nil, true, err
	}
	kernel.nextID = max(kernel.nextID, checkpoint.Envelope.Sequence) + 1
	request := frame{Type: "restore_checkpoint", ID: kernel.nextID, Bytes: len(checkpoint.Data), SHA256: checkpoint.Envelope.SHA256}
	if err := writeFrame(kernel.worker.input, kernel.limits.FrameBytes, request); err != nil {
		return nil, true, err
	}
	if err := writeBlobChunks(kernel.worker.input, kernel.limits.FrameBytes, request.ID, checkpoint.Data); err != nil {
		return nil, true, err
	}
	response, err := kernel.read(loadCtx)
	if err != nil {
		return nil, true, err
	}
	if response.ID != request.ID || response.Type != "result" {
		return nil, true, errors.New("mismatched checkpoint restore response")
	}
	if response.Error != "" {
		return nil, true, errors.New(response.Error)
	}
	var report RestoreReport
	if err := decodeFrameValue(response.Value, &report); err != nil {
		return nil, true, err
	}
	if report.Restored == nil {
		return nil, true, errors.New("invalid checkpoint restore report")
	}
	report.Failed = append(report.Failed, checkpoint.Envelope.Manifest.Skipped...)
	kernel.needsRestore = false
	kernel.snapshotHash = checkpointContentHash(checkpoint.Data, checkpoint.Envelope.Manifest)
	kernel.skippedHash = scratchSkippedHash(checkpoint.Envelope.Manifest.Skipped)
	if kernel.onRestore != nil {
		kernel.onRestore(ctx, report)
	}
	return &report, true, nil
}

func (kernel *Kernel) captureCheckpointLocked(ctx context.Context) *ScratchReport {
	if kernel.worker == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), max(kernel.limits.Wall, 5*time.Second))
	defer cancel()
	warning := func(err error) *ScratchReport {
		return &ScratchReport{Warning: noticeText("Scratch checkpoint failed; completed effects remain. Do not replay effects. The last committed image is retained: "+err.Error(), 1024)}
	}
	kernel.nextID++
	id := kernel.nextID
	if err := writeFrame(kernel.worker.input, kernel.limits.FrameBytes, frame{Type: "checkpoint", ID: id}); err != nil {
		kernel.stop()
		return warning(err)
	}
	begin, err := kernel.read(ctx)
	if err != nil {
		kernel.stop()
		return warning(err)
	}
	if begin.Type == "result" && begin.ID == id && begin.Error != "" {
		return warning(errors.New(begin.Error))
	}
	if begin.Type != "checkpoint_begin" || begin.ID != id {
		kernel.stop()
		return warning(errors.New("invalid checkpoint begin"))
	}
	data, err := receiveBlob(begin, func() (frame, error) { return kernel.read(ctx) })
	if err != nil {
		kernel.stop()
		return warning(err)
	}
	response, err := kernel.read(ctx)
	if err != nil {
		kernel.stop()
		return warning(err)
	}
	if response.Type != "result" || response.ID != id || response.Error != "" {
		kernel.stop()
		return warning(fmt.Errorf("invalid checkpoint result: %s", response.Error))
	}
	var manifest SnapshotManifest
	if err := decodeFrameValue(response.Value, &manifest); err != nil {
		return warning(err)
	}
	hash := checkpointContentHash(data, manifest)
	if hash == kernel.snapshotHash {
		return nil
	}
	checkpoint := newCheckpoint(kernel.engine, id, data, manifest)
	if err := kernel.checkpoints.Save(ctx, checkpoint); err != nil {
		return warning(err)
	}
	kernel.snapshotHash = hash
	skippedHash := scratchSkippedHash(manifest.Skipped)
	changed := skippedHash != kernel.skippedHash && (len(manifest.Skipped) > 0 || kernel.skippedHash != [32]byte{})
	kernel.skippedHash = skippedHash
	if changed {
		return &ScratchReport{Skipped: append([]SkippedName{}, manifest.Skipped...)}
	}
	return nil
}

// A partial Starlark image can stay byte-identical while its omissions change.
// Persist and report both data and manifest changes, as in the legacy codec.
func checkpointContentHash(data []byte, manifest SnapshotManifest) [32]byte {
	digest := sha256.Sum256(data)
	encoded, _ := json.Marshal(manifest)
	return sha256.Sum256(append(digest[:], encoded...))
}
