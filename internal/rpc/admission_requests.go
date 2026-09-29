package rpc

import (
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func submissionRequest(p protocol.SubmitParams) store.Submission {
	parts := make([]session.Part, len(p.Parts))
	for i, part := range p.Parts {
		parts[i] = part.Domain()
	}
	request := store.Submission{DesignContext: p.DesignContext.Domain(), SessionID: session.SessionID(p.SessionID), Source: session.InputSource(p.Source), Parts: parts, Delivery: session.InputDelivery(p.Delivery)}
	if p.TargetTurnID != nil {
		request.TargetTurnID = new(session.TurnID(*p.TargetTurnID))
	}
	return request
}

func childRequest(p protocol.SpawnSessionParams) (store.ChildRequest, error) {
	patch, err := p.Overrides.Domain()
	if err != nil {
		return store.ChildRequest{}, err
	}
	request := store.ChildRequest{Name: p.Name, Template: p.Template, ParentID: session.SessionID(p.ParentID), Overrides: patch, Resources: protocol.ResourceLimitsDomain(p.Resources)}
	for _, id := range p.BrowserAttachments {
		request.BrowserAttachments = append(request.BrowserAttachments, string(id))
	}
	for _, limit := range p.Budgets {
		request.Budgets = append(request.Budgets, limit.Domain())
	}
	for _, part := range p.Parts {
		request.Parts = append(request.Parts, part.Domain())
	}
	if p.GrantIDs != nil {
		request.GrantIDs = make([]session.GrantID, len(p.GrantIDs))
		for i, id := range p.GrantIDs {
			request.GrantIDs[i] = session.GrantID(id)
		}
	}
	if p.Definition != nil {
		ref := definitionRef(*p.Definition)
		request.Definition = &ref
	}
	if p.WorkingDirectory != nil {
		request.WorkingDirectory = *p.WorkingDirectory
	}
	return request, nil
}
