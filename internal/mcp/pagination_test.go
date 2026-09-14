package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// pagedLister answers tools/list from a scripted cursor chain and records the
// cursors it was asked for.
type pagedLister struct {
	pages   map[string]*sdkmcp.ListToolsResult // keyed by requested cursor ("" first)
	asked   []string
	failAt  string
	endless bool
}

func (p *pagedLister) ListTools(_ context.Context, params *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error) {
	cursor := ""
	if params != nil {
		cursor = params.Cursor
	}
	p.asked = append(p.asked, cursor)
	if p.failAt != "" && cursor == p.failAt {
		return nil, errors.New("page unavailable")
	}
	if p.endless {
		n := len(p.asked)
		return &sdkmcp.ListToolsResult{Tools: []*sdkmcp.Tool{{Name: fmt.Sprintf("t%d", n)}}, NextCursor: fmt.Sprintf("c%d", n)}, nil
	}
	res, ok := p.pages[cursor]
	if !ok {
		return nil, fmt.Errorf("unknown cursor %q", cursor)
	}
	return res, nil
}

func TestListAllToolsFollowsCursors(t *testing.T) {
	lister := &pagedLister{pages: map[string]*sdkmcp.ListToolsResult{
		"":   {Tools: []*sdkmcp.Tool{{Name: "a"}}, NextCursor: "p2"},
		"p2": {Tools: []*sdkmcp.Tool{{Name: "b"}}, NextCursor: "p3"},
		"p3": {Tools: []*sdkmcp.Tool{{Name: "c"}}},
	}}
	tools, err := listAllTools(context.Background(), lister)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "a,b,c" {
		t.Fatalf("tools = %v, want every page in order", names)
	}
	if strings.Join(lister.asked, ",") != ",p2,p3" {
		t.Fatalf("cursors asked = %q", lister.asked)
	}
}

func TestListAllToolsSurfacesPageErrors(t *testing.T) {
	lister := &pagedLister{failAt: "p2", pages: map[string]*sdkmcp.ListToolsResult{
		"": {Tools: []*sdkmcp.Tool{{Name: "a"}}, NextCursor: "p2"},
	}}
	if _, err := listAllTools(context.Background(), lister); err == nil || !strings.Contains(err.Error(), "page unavailable") {
		t.Fatalf("err = %v, want the page error (a partial catalog must not be published)", err)
	}
}

func TestListAllToolsRejectsCursorCycle(t *testing.T) {
	lister := &pagedLister{pages: map[string]*sdkmcp.ListToolsResult{
		"":     {Tools: []*sdkmcp.Tool{{Name: "a"}}, NextCursor: "loop"},
		"loop": {Tools: []*sdkmcp.Tool{{Name: "b"}}, NextCursor: "loop"},
	}}
	_, err := listAllTools(context.Background(), lister)
	if err == nil || !strings.Contains(err.Error(), `cursor "loop" repeats`) {
		t.Fatalf("err = %v, want a repeated-cursor error", err)
	}
	if len(lister.asked) != 2 {
		t.Fatalf("asked %d pages, want the loop caught on the second", len(lister.asked))
	}
}

func TestListAllToolsBoundsPageCount(t *testing.T) {
	lister := &pagedLister{endless: true}
	_, err := listAllTools(context.Background(), lister)
	if err == nil || !strings.Contains(err.Error(), "exceeds 64 pages") {
		t.Fatalf("err = %v, want the page bound", err)
	}
	if len(lister.asked) != maxToolPages {
		t.Fatalf("asked %d pages, want exactly %d before giving up", len(lister.asked), maxToolPages)
	}
}

// TestManagerLoadsEveryToolPage drives the real connect path against an
// in-process server that pages one tool at a time: the published catalog is
// the whole catalog.
func TestManagerLoadsEveryToolPage(t *testing.T) {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "paged"}, &sdkmcp.ServerOptions{PageSize: 1})
	for _, name := range []string{"one", "two", "three"} {
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}},
			func(ctx context.Context, req *sdkmcp.CallToolRequest, in struct{}) (*sdkmcp.CallToolResult, any, error) {
				return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil, nil
			})
	}
	m := NewManager(map[string]ServerConfig{"paged": testCfg("paged")})
	m.connectTransport = func(context.Context, ServerConfig, *ringBuffer) (sdkmcp.Transport, error) {
		clientT, serverT := sdkmcp.NewInMemoryTransports()
		ss, err := srv.Connect(context.Background(), serverT, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { m.Close(); ss.Close() }) // client first, like serveTestServer
		return clientT, nil
	}
	t.Cleanup(m.Close)
	m.Start(context.Background())
	waitReady(t, m)
	if st := m.Statuses()[0]; st.Status != StatusReady || st.Tools != 3 {
		t.Fatalf("status = %+v, want ready with all 3 paged tools", st)
	}
}
