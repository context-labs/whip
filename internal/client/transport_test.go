package client

import (
	"io"

	"github.com/context-labs/whip/internal/protocoltransport"
)

func writeProtocolMessage(writer io.Writer, message protocoltransport.Message) error {
	frame, err := protocoltransport.MarshalFrame(message)
	if err != nil {
		return err
	}
	_, err = writer.Write(frame)
	return err
}
