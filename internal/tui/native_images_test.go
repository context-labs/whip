package tui

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	hostmodel "github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
)

func nativeImageFixture(t *testing.T) []byte {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, 3, 2))
	value.Set(1, 1, color.RGBA{R: 255, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, value); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestNativeImagePasteCopiesLocalSourceAndJournalsOwnedReference(t *testing.T) {
	m, provider := nativeUIFixture(t)
	provider.requests = make(chan hostmodel.Request, 4)
	m.recovery = nativeJournal(t, t.TempDir(), m.connection.Identity())
	m.clientDirectory = t.TempDir()
	data := nativeImageFixture(t)
	path := filepath.Join(m.clientDirectory, "Screenshot [original]")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("Look at ")
	_, load := m.Update(tea.PasteMsg{Content: path + "\n"})
	if load == nil {
		t.Fatal(m.status)
	}
	loaded := load().(nativeImageLoaded)
	if loaded.err != nil || !bytes.Equal(loaded.data, data) {
		t.Fatal(loaded.err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, upload := m.Update(loaded)
	if upload == nil {
		t.Fatal(m.status)
	}
	result := upload().(nativeImageUploaded)
	m.Update(result)
	if result.err != nil || len(m.images) != 1 || m.attachment != nil {
		t.Fatal(result.err, m.status)
	}
	if strings.Contains(m.input.Value(), path) || !strings.Contains(m.input.Value(), "Screenshot (original)") {
		t.Fatal(m.input.Value())
	}
	reference, body, err := m.handle.ReadContent(t.Context(), result.reference.ID)
	if err != nil || reference.SessionID != m.owner.ID || !bytes.Equal(body, data) {
		t.Fatal(reference, err)
	}
	page, err := m.handle.History(t.Context(), protocol.HistoryPageParams{Direction: "forward", Limit: 10})
	if err != nil || len(page.Messages) != 0 {
		t.Fatal("upload admitted a prompt", page, err)
	}
	command := m.submit()
	if command == nil {
		t.Fatal(m.status)
	}
	retained, err := m.recovery.restore(m.connection, m.owner.ID)
	if err != nil || retained == nil {
		t.Fatal(err)
	}
	var params protocol.SubmitParams
	if err := json.Unmarshal(retained.Record().Params, &params); err != nil || len(params.Parts) != 2 || params.Parts[0].Text != "Look at " || params.Parts[1].ReferenceID != reference.ID || strings.Contains(string(retained.Record().Params), path) {
		t.Fatal(params, err)
	}
	admitted := command().(nativeSubmission)
	m.Update(admitted)
	if admitted.err != nil || !reflect.DeepEqual(admitted.admission.Input.Parts, params.Parts) {
		t.Fatal(admitted)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	settled, err := admitted.command.Wait(ctx)
	if err != nil || settled.Turn.State != "succeeded" {
		t.Fatal(settled, err)
	}
	select {
	case request := <-provider.requests:
		content := request.Contents[string(reference.ID)]
		if content.MediaType != "image/png" || !bytes.Equal(content.Data, data) {
			t.Fatal("provider did not receive canonical image bytes", content)
		}
	case <-ctx.Done():
		t.Fatal("provider never received image request")
	}
	page, err = m.handle.History(ctx, protocol.HistoryPageParams{Direction: "forward", Limit: 10})
	if err != nil || !reflect.DeepEqual(page.Messages[0].Parts, params.Parts) {
		t.Fatal(page, err)
	}
}

func TestNativeImageUnknownAcknowledgementKeepsExactOwnerAndBody(t *testing.T) {
	m, _ := nativeUIFixture(t)
	other := nativeNavigationRoot(t, m, "image-other", "Other")
	data := nativeImageFixture(t)
	m.attachment = &nativeImageUpload{owner: m.owner.ID, id: "unknown-image", media: "image/png", data: data}
	original := m.attachment
	result := m.uploadImage("send")().(nativeImageUploaded)
	if result.err != nil {
		t.Fatal(result.err)
	}
	// SQL committed the body; simulate only its acknowledgement being lost.
	m.Update(nativeImageUploaded{upload: original, err: io.ErrUnexpectedEOF})
	if m.attachment != original || m.attachmentBusy || !bytes.Equal(m.attachment.data, data) {
		t.Fatal("unknown upload replaced immutable body")
	}
	if err := m.attachSession(other); err == nil {
		t.Fatal("unknown upload retargeted")
	}
	m.input.SetValue("inspect this")
	if m.submit() != nil || m.command("/cd /tmp") != nil {
		t.Fatal("unresolved upload allowed another action")
	}
	checked := m.attachCommand("check")().(nativeImageUploaded)
	m.Update(checked)
	if checked.err != nil || checked.reference.ID != original.id || m.attachment != nil || len(m.images) != 1 {
		t.Fatal(checked, m.status)
	}
	root := m.owner
	draft := m.input.Value()
	if err := m.attachSession(other); err != nil || len(m.images) != 0 {
		t.Fatal(err)
	}
	if _, _, err := m.handle.ReadContent(t.Context(), original.id); err == nil {
		t.Fatal("image leaked through another owner")
	}
	parts, err := m.promptParts(draft)
	if err != nil || len(parts) != 1 || parts[0].Type != "text" {
		t.Fatal("unregistered chip attached", parts, err)
	}
	if err := m.attachSession(root); err != nil || m.input.Value() != draft || len(m.images) != 1 {
		t.Fatal("owner image draft lost", err)
	}
	if m.directShell("echo "+draft) != nil || !strings.Contains(m.status, "does not accept") {
		t.Fatal("image silently discarded from shell", m.status)
	}
	m.attachment = &nativeImageUpload{owner: m.owner.ID, id: "not-uploaded", media: "image/png", data: data}
	missing := m.attachCommand("check")().(nativeImageUploaded)
	m.Update(missing)
	if missing.err == nil || m.attachment == nil {
		t.Fatal("missing check sent content", missing)
	}
	retried := m.attachCommand("retry")().(nativeImageUploaded)
	m.Update(retried)
	if retried.err != nil || retried.reference.ID != "not-uploaded" {
		t.Fatal(retried)
	}
}

func TestNativeImagePromptAssemblyAndRejectedRedraft(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.preferences.CollapsePaste = new(true)
	m.pasteText("first\tline\nsecond\n third")
	m.addImage(protocol.ContentReference{ID: "image-ref", SessionID: m.owner.ID}, "test.png")
	m.pasteText(" after\nimage\n ")
	want, err := m.promptParts(m.input.Value())
	if err != nil || len(want) != 3 || want[1].ReferenceID != "image-ref" {
		t.Fatal(want, err)
	}
	command, err := m.handle.Submission(protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "tui", RequestID: "rejected-image"}, Source: "user", Parts: want})
	if err != nil {
		t.Fatal(err)
	}
	m.rejected = command
	if !m.restoreRejectedDraft() {
		t.Fatal("image redraft refused")
	}
	got, err := m.promptParts(m.input.Value())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, want, err)
	}
	chip := ""
	for key := range m.images {
		chip = key
	}
	m.input.SetValue(strings.Repeat(chip, 9))
	if _, err := m.promptParts(m.input.Value()); err == nil {
		t.Fatal("repeated image exceeded limit")
	}
	m.images[chip] = nativeImage{reference: protocol.ContentReference{ID: "other-ref", SessionID: "other-owner"}}
	if _, err := m.promptParts(chip); err == nil {
		t.Fatal("foreign image reference entered prompt")
	}
}

func TestNativeImageFileBoundsAndLocalPathRules(t *testing.T) {
	directory := t.TempDir()
	data := nativeImageFixture(t)
	path := filepath.Join(directory, "image with space.png")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"image with space.png", `image\ with\ space.png`, "file://localhost" + strings.ReplaceAll(path, " ", "%20")} {
		got, err := nativeLocalPath(value, directory)
		if err != nil || got != path {
			t.Fatal(value, got, err)
		}
	}
	if _, err := nativeLocalPath("file://remote/image.png", directory); err == nil {
		t.Fatal("remote file URL accepted")
	}
	linked := filepath.Join(directory, "linked.png")
	if err := os.Symlink(path, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := nativeReadImage(linked); err == nil {
		t.Fatal("symlink followed")
	}
	fifo := filepath.Join(directory, "fifo.png")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := nativeReadImage(fifo); err == nil {
		t.Fatal("FIFO accepted")
	}
	large := filepath.Join(directory, "large.png")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(nativeImageReadLimit + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := nativeReadImage(large); err == nil {
		t.Fatal("oversized regular file accepted")
	}
	if _, _, err := nativeNormalizeImage([]byte("not an image")); !errors.Is(err, errNativeNotImage) {
		t.Fatal(err)
	}
	bomb := bytes.Clone(data)
	binary.BigEndian.PutUint32(bomb[16:20], 100000)
	binary.BigEndian.PutUint32(bomb[20:24], 100000)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	if _, _, err := nativeNormalizeImage(bomb); err == nil || !strings.Contains(err.Error(), "64 megapixel") {
		t.Fatal("oversized canvas reached pixel decode", err)
	}
	media, body, err := nativeNormalizeImage(data)
	if err != nil || media != "image/png" || !bytes.Equal(data, body) {
		t.Fatal(media, err)
	}
}
