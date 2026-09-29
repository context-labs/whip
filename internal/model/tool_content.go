package model

import (
	"errors"

	"github.com/context-labs/whip/internal/session"
)

// providerMessages leaves canonical tool history intact. Chat/Responses accept
// images as user input, so complete a consecutive group of tool results first,
// then send its trusted attachments as one wire-only user content message.
func providerMessages(source []Message) ([]Message, error) {
	messages := make([]Message, 0, len(source))
	var images []session.Part
	flush := func() {
		if len(images) > 0 {
			messages = append(messages, Message{Role: session.User, Parts: images})
			images = nil
		}
	}
	for _, message := range source {
		if err := session.ValidateMessage(message.Role, message.Parts); err != nil {
			return nil, err
		}
		if message.Role != session.Tool {
			flush()
			messages = append(messages, message)
			continue
		}
		images = append(images, message.Parts[1:]...)
		message.Parts = message.Parts[:1]
		messages = append(messages, message)
	}
	flush()
	if len(messages) > 100 {
		return nil, errors.New("provider message count exceeds limit including tool images")
	}
	return messages, nil
}
