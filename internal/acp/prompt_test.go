package acp

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestNativePromptPreservesTextReferencesAndImages(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	blocks := []acp.ContentBlock{
		acp.TextBlock("hello"),
		{ResourceLink: &acp.ContentBlockResourceLink{Type: "resource_link", Uri: "file:///private/notes", Name: "notes"}},
		{Image: &acp.ContentBlockImage{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(data.Bytes())}},
		{Resource: &acp.ContentBlockResource{Type: "resource", Resource: acp.EmbeddedResourceResource{TextResourceContents: &acp.TextResourceContents{Uri: "file:///editor.ts", Text: "let x = 1"}}}},
	}
	result, err := preparePrompt(blocks, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.text != "hello\n\n@file:///private/notes\n\nFile: file:///editor.ts\n```\nlet x = 1\n```" || len(result.images) != 1 || !bytes.Equal(result.images[0].data, data.Bytes()) || result.images[0].media != "image/png" {
		t.Fatalf("result=%+v", result)
	}
}

func TestNativePromptRejectsBoundsBeforePublication(t *testing.T) {
	imageBlock := acp.ContentBlock{Image: &acp.ContentBlockImage{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("image"))}}
	cases := map[string][]acp.ContentBlock{
		"empty":               nil,
		"too many blocks":     make([]acp.ContentBlock, 129),
		"text":                {acp.TextBlock(strings.Repeat("a", 1<<20))},
		"escaped text":        {acp.TextBlock(strings.Repeat("\t", 600000))},
		"NUL":                 {acp.TextBlock("a\x00b")},
		"invalid utf8":        {acp.TextBlock(string([]byte{0xff}))},
		"images":              {imageBlock, imageBlock, imageBlock, imageBlock, imageBlock, imageBlock, imageBlock, imageBlock, imageBlock},
		"large encoded image": {{Image: &acp.ContentBlockImage{Type: "image", MimeType: "image/png", Data: strings.Repeat("A", base64.StdEncoding.EncodedLen(8<<20)+1)}}},
		"unsupported media":   {{Image: &acp.ContentBlockImage{Type: "image", MimeType: "image/svg+xml", Data: "PHN2Zy8+"}}},
	}
	for name, blocks := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := preparePrompt(blocks, true); err == nil {
				t.Fatal("accepted out-of-bounds prompt")
			}
		})
	}
}

func TestNativePromptUnsupportedBlocksRemainVisible(t *testing.T) {
	result, err := preparePrompt([]acp.ContentBlock{
		{Image: &acp.ContentBlockImage{Type: "image", MimeType: "image/png", Data: "!!!"}},
		{Audio: &acp.ContentBlockAudio{Type: "audio", MimeType: "audio/mpeg"}},
		{Resource: &acp.ContentBlockResource{Type: "resource", Resource: acp.EmbeddedResourceResource{BlobResourceContents: &acp.BlobResourceContents{Uri: "file:///binary"}}}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.images) != 0 || result.text != "[image: image/png]\n\n[audio: audio/mpeg — not supported]\n\n[binary resource: file:///binary]" {
		t.Fatalf("result=%+v", result)
	}
}
