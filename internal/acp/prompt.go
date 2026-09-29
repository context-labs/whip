package acp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/imageutil"
	"github.com/context-labs/whip/internal/protocol"
)

type (
	promptImage struct {
		media string
		data  []byte
	}
	promptContent struct {
		text   string
		images []promptImage
	}
)

// preparePrompt performs all local bounds checks before uploading any content.
// URI references are text only: the editor cannot make this adapter read a file.
func preparePrompt(blocks []acp.ContentBlock, vision bool) (promptContent, error) {
	if len(blocks) == 0 || len(blocks) > 128 {
		return promptContent{}, errors.New("ACP prompt requires 1..128 blocks")
	}
	var text strings.Builder
	result := promptContent{}
	rawBytes, imageBytes := 0, 0
	add := func(value string) error {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return errors.New("ACP text requires valid UTF-8 without NUL")
		}
		if text.Len()+len(value)+2 > 1<<20 {
			return errors.New("ACP prompt text exceeds 1 MiB")
		}
		if text.Len() != 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(value)
		return nil
	}
	for _, block := range blocks {
		value := ""
		switch {
		case block.Text != nil:
			value = block.Text.Text
		case block.ResourceLink != nil:
			value = "@" + block.ResourceLink.Uri
		case block.Resource != nil:
			resource := block.Resource.Resource
			switch {
			case resource.TextResourceContents != nil:
				r := resource.TextResourceContents
				if len(r.Text)+len(r.Uri) > 1<<20 {
					return result, errors.New("ACP embedded text exceeds 1 MiB")
				}
				value = fmt.Sprintf("File: %s\n```\n%s\n```", r.Uri, r.Text)
			case resource.BlobResourceContents != nil:
				value = "[binary resource: " + resource.BlobResourceContents.Uri + "]"
			}
		case block.Image != nil:
			image := block.Image
			rawBytes += len(image.Data)
			if len(image.Data) > base64.StdEncoding.EncodedLen(8<<20) || rawBytes > base64.StdEncoding.EncodedLen(16<<20) {
				return result, errors.New("ACP encoded images exceed the 8 MiB individual or 16 MiB aggregate input bound")
			}
			if vision {
				media := image.MimeType
				if media == "image/jpg" {
					media = "image/jpeg"
				}
				switch media {
				case "image/png", "image/jpeg", "image/webp", "image/gif":
				default:
					return result, fmt.Errorf("unsupported ACP image media type %q", media)
				}
				data, err := base64.StdEncoding.DecodeString(image.Data)
				if err == nil {
					ext, data := imageutil.NormalizeImage(strings.TrimPrefix(media, "image/"), data)
					if ext == "jpg" {
						ext = "jpeg"
					}
					if len(data) == 0 || len(data) > 4<<20 {
						return result, errors.New("ACP normalized image must contain 1 byte..4 MiB")
					}
					imageBytes += len(data)
					if len(result.images) >= 8 || imageBytes > 16<<20 {
						return result, errors.New("ACP prompt exceeds 8 images or 16 MiB of normalized content")
					}
					result.images = append(result.images, promptImage{media: "image/" + ext, data: data})
					continue
				}
			}
			value = "[image: " + image.MimeType + "]"
		case block.Audio != nil:
			value = "[audio: " + block.Audio.MimeType + " — not supported]"
		}
		if value != "" {
			if err := add(value); err != nil {
				return result, err
			}
		}
	}
	result.text = text.String()
	// Escaped text and JSON structure also count toward the host input bound.
	probe := protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "acp", RequestID: "validation"}, SessionID: "validation", Source: "user", Parts: []protocol.Part{}}
	if result.text != "" {
		probe.Parts = append(probe.Parts, protocol.Part{Type: "text", Text: result.text})
	}
	for range result.images {
		probe.Parts = append(probe.Parts, protocol.Part{Type: "content", ReferenceID: protocol.ID(strings.Repeat("x", 128))})
	}
	raw, err := json.Marshal(probe)
	if err != nil {
		return result, err
	}
	if len(raw) > (1<<20)-1024 {
		return result, errors.New("ACP prompt exceeds the 1 MiB input document bound")
	}
	if err := protocol.Validate("SubmitParams", raw); err != nil {
		return result, err
	}
	return result, nil
}
