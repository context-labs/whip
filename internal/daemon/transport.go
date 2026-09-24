package daemon

import (
	"bufio"
	"io"
	"net"

	"github.com/context-labs/whip/internal/protocoltransport"
)

type messageTransport = protocoltransport.Transport

func newUnixMessageTransport(conn net.Conn) *protocoltransport.Unix {
	return protocoltransport.NewUnix(conn)
}

func newWebsocketMessageTransport(conn net.Conn, source io.Reader) *protocoltransport.WebSocket {
	return protocoltransport.NewWebSocket(conn, source)
}

func readProtocolFrame(reader *bufio.Reader) ([]byte, error) {
	return protocoltransport.ReadFrame(reader)
}

func writeTransportMessage(transport messageTransport, message rpcMessage) error {
	frame, err := marshalFrame(message)
	if err != nil {
		return err
	}
	return transport.WriteMessage(frame)
}
