package tui

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/imageutil"
	"github.com/context-labs/whip/internal/protocol"
)

const nativeImageReadLimit = 16 << 20

var errNativeNotImage = errors.New("file has no supported image header")

type nativeImage struct{ reference protocol.ContentReference }

// An uncertain upload retains one immutable body and handle. It is not an
// accepted prompt: only the separately journaled InputRecord can admit work.
type nativeImageUpload struct {
	owner, id   protocol.ID
	media, name string
	data        []byte
}

type nativeImageLoaded struct {
	generation  uint64
	owner       protocol.ID
	media, name string
	data        []byte
	fallback    string
	err         error
}

type nativeImageUploaded struct {
	upload    *nativeImageUpload
	reference protocol.ContentReference
	err       error
}

func (m *nativeModel) liveImages(text string) map[string]nativeImage {
	live := maps.Clone(m.images)
	for chip := range live {
		if !strings.Contains(text, chip) {
			delete(live, chip)
		}
	}
	return live
}

func (m *nativeModel) attachCommand(args string) tea.Cmd {
	if m.attachmentBusy {
		m.status = "An image is being read or uploaded; wait for its result before another attachment action."
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/attach") {
		m.input.Reset()
	}
	switch args {
	case "check", "retry":
		if m.attachment == nil {
			m.status = "No image upload is pending."
			return nil
		}
		return m.uploadImage(args)
	case "discard":
		m.attachment = nil
		m.status = "Local pending upload forgotten; no prompt was sent. Any uploaded content remains owned by its original session."
		return nil
	case "":
		m.status = "usage: /attach <client-local path> | clipboard | check | retry | discard"
		return nil
	}
	if m.attachment != nil || !m.nativeAdmissionAvailable() {
		m.status = "Resolve the original pending action or image upload before another attachment."
		return nil
	}
	if args == "clipboard" {
		return m.loadImage("", "")
	}
	path, err := nativeLocalPath(args, m.clientDirectory)
	if err != nil {
		m.status = "Image attachment: " + err.Error()
		return nil
	}
	return m.loadImage(path, "")
}

func (m *nativeModel) loadImage(path, fallback string) tea.Cmd {
	if m.attachmentBusy || m.attachment != nil || !m.nativeAdmissionAvailable() {
		m.status = "Resolve the original pending action before loading another image."
		return nil
	}
	m.images = m.liveImages(m.input.Value())
	if len(m.images) >= 8 {
		m.status = "Image refused: eight attachments are already in this draft. Remove one first."
		return nil
	}
	m.attachmentBusy = true
	m.status = "Reading a client-local image…"
	owner, generation, directory := m.owner.ID, m.generation, m.clientDirectory
	return func() tea.Msg {
		result := nativeImageLoaded{owner: owner, generation: generation}
		ctx, done, err := m.work.beginFor(30 * time.Second)
		if err != nil {
			result.err = err
			return result
		}
		defer done()
		var data []byte
		if path == "" {
			data, err = nativeClipboardImage(ctx, directory)
		} else {
			data, err = nativeReadImage(path)
			result.name = filepath.Base(path)
		}
		if fallback != "" && (errors.Is(err, os.ErrNotExist) || errors.Is(err, errNativeNotImage)) {
			result.fallback = fallback
			return result
		}
		if err == nil {
			result.media, result.data, err = nativeNormalizeImage(data)
		}
		result.err = errors.Join(err, ctx.Err())
		return result
	}
}

func (m *nativeModel) imageLoaded(value nativeImageLoaded) tea.Cmd {
	if value.generation != m.generation || value.owner != m.owner.ID {
		return nil
	}
	m.attachmentBusy = false
	if value.fallback != "" {
		return m.pasteText(value.fallback)
	}
	if value.err != nil {
		m.status = "Image was not attached: " + value.err.Error()
		return nil
	}
	m.attachment = &nativeImageUpload{owner: value.owner, id: protocol.ID(uuid.NewString()), media: value.media, name: value.name, data: value.data}
	return m.uploadImage("send")
}

func (m *nativeModel) uploadImage(action string) tea.Cmd {
	upload, handle := m.attachment, m.handle
	if upload == nil || upload.owner != m.owner.ID {
		m.status = "Pending upload belongs to another session."
		return nil
	}
	m.attachmentBusy = true
	m.status = "Checking image content…"
	return func() tea.Msg {
		ctx, done, err := m.work.begin()
		if err != nil {
			return nativeImageUploaded{upload: upload, err: err}
		}
		defer done()
		var reference protocol.ContentReference
		if action == "check" {
			var data []byte
			reference, data, err = handle.ReadContent(ctx, upload.id)
			if err == nil && (!bytes.Equal(data, upload.data) || reference.MediaType != upload.media) {
				err = errors.New("uploaded image differs from the original pending body")
			}
		} else {
			reference, err = handle.PutContent(ctx, upload.id, upload.media, upload.data)
		}
		return nativeImageUploaded{upload: upload, reference: reference, err: err}
	}
}

func (m *nativeModel) imageUploaded(value nativeImageUploaded) {
	if value.upload != m.attachment || value.upload.owner != m.owner.ID {
		return
	}
	m.attachmentBusy = false
	if value.err != nil {
		m.status = "Image upload not confirmed: " + value.err.Error() + ". /attach check reads the original handle; /attach retry sends the same bytes; /attach discard forgets only this local upload."
		return
	}
	if value.reference.SessionID != m.owner.ID || value.reference.ID != value.upload.id {
		m.status = "Image upload returned a different owner or handle; the original upload remains pending."
		return
	}
	m.addImage(value.reference, value.upload.name)
	m.attachment = nil
	m.status = "Image attached to this draft. It is not sent until the prompt is submitted."
	m.sizeInput()
}

func (m *nativeModel) addImage(reference protocol.ContentReference, name string) {
	m.addContentChip(reference, name, "Image")
}

func (m *nativeModel) addContentChip(reference protocol.ContentReference, name, kind string) {
	m.images = m.liveImages(m.input.Value())
	if m.images == nil {
		m.images = map[string]nativeImage{}
	}
	m.imageSequence++
	label := ""
	if name != "" {
		name = strings.NewReplacer("[", "(", "]", ")", "\n", " ").Replace(nativeDisplayText(name))
		label = ": " + ansi.Truncate(name, 24, "…")
	}
	chip := fmt.Sprintf("[%s %d%s\u200b]", kind, m.imageSequence, label)
	m.images[chip] = nativeImage{reference: reference}
	m.input.InsertString(chip)
}

func (m *nativeModel) promptParts(text string) ([]protocol.Part, error) {
	var parts []protocol.Part
	bytes, images := 0, 0
	for text != "" {
		at, found := len(text), ""
		for chip := range m.images {
			if index := strings.Index(text, chip); index >= 0 && index < at {
				at, found = index, chip
			}
		}
		segment, err := m.expandPastes(text[:at])
		if err != nil {
			return nil, err
		}
		bytes += len(segment)
		if bytes > nativeDraftLimit {
			return nil, errors.New("draft exceeds the 256 KiB text limit")
		}
		if segment != "" {
			parts = append(parts, protocol.Part{Type: "text", Text: segment})
		}
		if found == "" {
			break
		}
		reference := m.images[found].reference
		if reference.SessionID != m.owner.ID || reference.ID == "" {
			return nil, errors.New("image reference does not belong to this draft owner")
		}
		images++
		if images > 8 {
			return nil, errors.New("a prompt may contain at most eight images")
		}
		parts = append(parts, protocol.Part{Type: "content", ReferenceID: reference.ID})
		text = text[at+len(found):]
	}
	return parts, nil
}

func (m *nativeModel) restoreParts(parts []protocol.Part) bool {
	bytes, images, texts := 0, 0, 0
	for _, part := range parts {
		switch part.Type {
		case "text":
			bytes += len(part.Text)
			texts++
		case "content":
			if part.ReferenceID == "" {
				return false
			}
			images++
		default:
			return false
		}
	}
	if bytes > nativeDraftLimit || images > 8 || texts > 9 {
		return false
	}
	// Build separately so a presentation bound cannot erase the rejected
	// original. Collapse only whitespace the editor cannot represent verbatim.
	draft := nativeModel{owner: m.owner, input: newInput(), width: m.width, height: m.height, pasteSequence: m.pasteSequence, imageSequence: m.imageSequence}
	for _, part := range parts {
		if part.Type == "text" {
			draft.pasteText(part.Text)
		} else {
			draft.addContentChip(protocol.ContentReference{ID: part.ReferenceID, SessionID: m.owner.ID}, "restored", "Attachment")
		}
	}
	actual, err := draft.promptParts(draft.input.Value())
	if err != nil || len(actual) != len(parts) {
		return false
	}
	for i, part := range parts {
		if actual[i].Type != part.Type || actual[i].Text != part.Text || actual[i].ReferenceID != part.ReferenceID {
			return false
		}
	}
	m.input, m.images, m.pastes = draft.input, draft.images, draft.pastes
	m.draftDesign = nil
	m.imageSequence, m.pasteSequence = draft.imageSequence, draft.pasteSequence
	m.sizeInput()
	return true
}

func nativePastedPath(text, directory string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	pathLike := filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "file:") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../")
	if strings.ContainsAny(trimmed, "\n\r") || !pathLike {
		return "", false
	}
	path, err := nativeLocalPath(trimmed, directory)
	return path, err == nil
}

func nativeLocalPath(value, directory string) (string, error) {
	if strings.HasPrefix(value, "file:") {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "file" || u.Host != "" && u.Host != "localhost" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return "", errors.New("attachment must be a client-local file URL")
		}
		value = u.Path
	} else {
		value = strings.ReplaceAll(value, `\ `, " ")
	}
	if !filepath.IsAbs(value) {
		if !filepath.IsAbs(directory) {
			return "", errors.New("client-local working directory is unavailable; use an absolute path")
		}
		value = filepath.Join(directory, value)
	}
	return filepath.Clean(value), nil
}

func nativeReadImage(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // Explicit client-local path; reject symlinks/special files and bound the body below.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > nativeImageReadLimit {
		return nil, errors.New("image must be a regular client-local file no larger than 16 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, nativeImageReadLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > nativeImageReadLimit {
		return nil, errors.New("image exceeded the 16 MiB read limit")
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return nil, errNativeNotImage
	}
	return data, nil
}

func nativeNormalizeImage(data []byte) (string, []byte, error) {
	if len(data) > nativeImageReadLimit {
		return "", nil, errors.New("image exceeds 16 MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", nil, errNativeNotImage
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > imageutil.NormalizeMaxPixels/config.Height {
		return "", nil, errors.New("image exceeds the 64 megapixel decode bound")
	}
	ext, data := imageutil.NormalizeImage(format, data)
	if ext == "jpg" {
		ext = "jpeg"
	}
	if len(data) > 4<<20 {
		return "", nil, errors.New("normalized image exceeds the complete-content limit of 4 MiB")
	}
	return "image/" + ext, data, nil
}
