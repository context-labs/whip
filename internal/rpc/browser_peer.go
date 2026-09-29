package rpc

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/browserhost"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

// browserConnection is entered only after ordinary initialization and an
// explicit first bind. It owns one reader and one notification pump; a write
// mutex serializes those two bounded producers without another retained queue.
// No accepted session execution borrows this connection's lifetime.
func (s *Server) browserConnection(parent context.Context, conn net.Conn, scanner *bufio.Scanner, first protocol.Request) {
	ctx, cancel := context.WithCancel(parent)
	peer, err := s.runtime.BrowserPeer()
	if err != nil {
		cancel()
		return
	}
	var pump sync.WaitGroup
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer func() {
		cancel()
		peer.Close()
		_ = conn.Close()
		pump.Wait()
		stop()
	}()
	var writer sync.Mutex
	write := func(value any) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if len(encoded) > protocol.MaxFrameBytes {
			return errors.New("browser frame exceeds bounds")
		}
		writer.Lock()
		defer writer.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}
		_, err = conn.Write(append(encoded, '\n'))
		return err
	}
	succeeded := false
	respond := func(request protocol.Request) error {
		requestCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		result, err := s.dispatchBrowserPeer(requestCtx, peer, request.Method, request.Params)
		stop()
		response := protocol.Response{JSONRPC: "2.0", ID: request.ID}
		if err == nil {
			response.Result, err = json.Marshal(result)
		}
		if err != nil {
			response.Result = nil
			response.Error = browserWireError(err)
		}
		succeeded = err == nil
		return write(response)
	}
	// The first binding acknowledgement precedes every command event on this
	// connection, so a native bridge can install its exact provider generation.
	if err := respond(first); err != nil || !succeeded {
		return
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return
	}
	pump.Go(func() {
		defer cancel()
		for {
			event, err := peer.Next(ctx)
			if err != nil {
				return
			}
			wire := browserEvent(event)
			if err := write(wire); err != nil {
				return
			}
		}
	})
	for scanner.Scan() {
		raw := scanner.Bytes()
		if protocol.Validate("Request", raw) != nil {
			return
		}
		var request protocol.Request
		if json.Unmarshal(raw, &request) != nil || respond(request) != nil {
			return
		}
	}
}

func (s *Server) dispatchBrowserPeer(ctx context.Context, peer *runtime.BrowserPeer, method string, raw json.RawMessage) (any, error) {
	var name string
	switch method {
	case "browser.provider.bind":
		name = "BrowserProviderBindParams"
	case "browser.provider.unbind":
		name = "BrowserProviderUnbindParams"
	case "browser.provider.event":
		name = "BrowserProviderEventParams"
	case "browser.command.result":
		name = "BrowserCommandResultParams"
	case "browser.screenshot.chunk":
		name = "BrowserScreenshotChunkParams"
	case "browser.inventory.result":
		name = "BrowserInventoryResultParams"
	default:
		return nil, ErrMethod
	}
	if err := protocol.Validate(name, raw); err != nil {
		return nil, session.ErrInvalid
	}
	switch method {
	case "browser.provider.bind":
		return decode(raw, func(p protocol.BrowserProviderBindParams) (any, error) {
			offer := browserhost.Offer{RootID: string(p.RootID), Version: p.Version, DesktopID: string(p.DesktopID), WindowID: string(p.WindowID), OfferRevision: string(p.OfferRevision), CreateProfileID: string(p.CreateProfileID), Availability: p.Availability, Tabs: []browserhost.OfferedTab{}, PreviewHosts: []browserhost.Preview{}}
			if p.ExpectedProviderEpoch != nil {
				offer.ExpectedProviderEpoch = string(*p.ExpectedProviderEpoch)
			}
			for _, tab := range p.OfferedTabs {
				offer.Tabs = append(offer.Tabs, browserhost.OfferedTab{TabID: string(tab.TabID), TabGeneration: string(tab.TabGeneration), ProfileID: string(tab.ProfileID), DocumentRevision: tab.DocumentRevision, URL: tab.URL, Title: tab.Title, Preview: browserPreviewToDomain(tab.Preview)})
			}
			for _, preview := range p.OfferedPreviewHosts {
				offer.PreviewHosts = append(offer.PreviewHosts, *browserPreviewToDomain(&preview))
			}
			binding, err := peer.Bind(ctx, offer)
			return protocol.BrowserProviderBindResult{Version: binding.Version, ProviderID: protocol.BrowserToken(binding.ProviderID), ProviderEpoch: protocol.BrowserToken(binding.ProviderEpoch)}, err
		})
	case "browser.provider.unbind":
		return decode(raw, func(p protocol.BrowserProviderUnbindParams) (any, error) {
			err := peer.Unbind(string(p.RootID), string(p.ProviderEpoch))
			return protocol.BrowserAccepted{Accepted: err == nil}, err
		})
	case "browser.provider.event":
		return decode(raw, func(p protocol.BrowserProviderEventParams) (any, error) {
			if p.Sequence <= 0 {
				return nil, session.ErrInvalid
			}
			event := browserhost.Event{RootID: string(p.RootID), ProviderEpoch: string(p.ProviderEpoch), TabID: string(p.TabID), TabGeneration: string(p.TabGeneration), AttachmentID: string(p.AttachmentID), AttachmentGeneration: string(p.AttachmentGeneration), Sequence: uint64(p.Sequence), DocumentRevision: p.DocumentRevision, Kind: p.Kind, Method: p.Method, Params: p.Params, URL: p.URL, Title: p.Title}
			if p.OperationID != nil {
				event.OperationID = string(*p.OperationID)
			}
			err := peer.Event(event)
			return protocol.BrowserAccepted{Accepted: err == nil}, err
		})
	case "browser.command.result":
		return decode(raw, func(p protocol.BrowserCommandResultParams) (any, error) {
			result := browserhost.CommandResult{CommandID: string(p.CommandID), RootID: string(p.RootID), ProviderEpoch: string(p.ProviderEpoch), AttachmentGeneration: string(p.AttachmentGeneration), DocumentRevision: p.DocumentRevision, URL: p.URL, Title: p.Title, Result: p.Result, Error: browserFailureToDomain(p.Error)}
			if p.Screenshot != nil {
				if p.Screenshot.Size > browserhost.MaxScreenshotBytes {
					return nil, session.ErrInvalid
				}
				result.Screenshot = &browserhost.Screenshot{Size: int(p.Screenshot.Size), Digest: p.Screenshot.Digest, MediaType: p.Screenshot.MediaType}
			}
			err := peer.Settle(result)
			return protocol.BrowserAccepted{Accepted: err == nil}, err
		})
	case "browser.screenshot.chunk":
		return decode(raw, func(p protocol.BrowserScreenshotChunkParams) (any, error) {
			if p.Offset > browserhost.MaxScreenshotBytes {
				return nil, session.ErrInvalid
			}
			data, err := base64.StdEncoding.Strict().DecodeString(p.DataBase64)
			if err != nil || len(data) == 0 || len(data) > 64<<10 {
				return nil, session.ErrInvalid
			}
			err = peer.UploadScreenshot(string(p.CommandID), string(p.RootID), string(p.ProviderEpoch), string(p.AttachmentGeneration), int(p.Offset), data)
			return protocol.BrowserAccepted{Accepted: err == nil}, err
		})
	case "browser.inventory.result":
		return decode(raw, func(p protocol.BrowserInventoryResultParams) (any, error) {
			result := browserhost.InventoryResult{RequestID: string(p.RequestID), RootID: string(p.RootID), ProviderEpoch: string(p.ProviderEpoch), Tabs: []browserhost.Tab{}, Error: browserFailureToDomain(p.Error)}
			for _, tab := range p.Tabs {
				value := browserhost.Tab{TabID: string(tab.TabID), TabGeneration: string(tab.TabGeneration), DocumentRevision: tab.DocumentRevision, URL: tab.URL, Title: tab.Title, State: tab.State, Requestable: tab.Requestable}
				if tab.AttachmentID != nil {
					value.AttachmentID = string(*tab.AttachmentID)
				}
				result.Tabs = append(result.Tabs, value)
			}
			err := peer.SettleInventory(result)
			return protocol.BrowserAccepted{Accepted: err == nil}, err
		})
	}
	return nil, ErrMethod
}

func browserWireError(err error) *protocol.RPCError {
	switch {
	case errors.Is(err, browserhost.ErrEventStale):
		return &protocol.RPCError{Code: -32009, Kind: "BROWSER_EVENT_STALE", Message: "browser observation belongs to a retired attachment"}
	case errors.Is(err, browserhost.ErrStale):
		return &protocol.RPCError{Code: -32009, Kind: "CONFLICT", Message: "browser connection or scope is stale"}
	case errors.Is(err, browserhost.ErrBusy):
		return &protocol.RPCError{Code: -32003, Kind: "BUSY", Message: "browser capacity reached"}
	case errors.Is(err, browserhost.ErrClosed):
		return &protocol.RPCError{Code: -32003, Kind: "CLOSED", Message: "browser connection closed"}
	default:
		return wireError(err)
	}
}
