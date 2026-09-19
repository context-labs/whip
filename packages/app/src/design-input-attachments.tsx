import { useCallback } from 'react';
import type { WhipClient } from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';
import type { DesignContext } from './browser-design-presentation';
import type { InboxInput } from './input-presentation';
import { BrowserDesignAttachment } from './browser-design-attachment';
import { InputAttachment } from './input-attachment';

/** Shared pending/queued presentation; identity, not a filename, groups evidence. */
export function DesignInputAttachments({ files, designContext, client, rootId, runtimeId, agentId, connected }: {
  files?: NonNullable<InboxInput['preview']>['attachments'];
  designContext?: DeepReadonly<DesignContext> | null;
  client: WhipClient; rootId: string; runtimeId: string; agentId: string; connected: boolean;
}) {
  const contextFile = files?.find(file => file.kind === 'text' && file.content.reference_id === designContext?.context_attachment_id);
  const screenshot = files?.find(file => file.kind === 'image' && file.content.reference_id === designContext?.screenshot_attachment_id);
  const valid = designContext && contextFile && (!designContext.screenshot_attachment_id || screenshot);
  const readContext = useCallback((signal: AbortSignal) => client.content(contextFile!.content, { rootId, agentId }).readText({ maxBytes: 65536, signal }), [client, contextFile, rootId, agentId]);
  const render = (file: NonNullable<typeof files>[number], index: number) => <InputAttachment key={`${file.content.reference_id}:${index}`} client={client} rootId={rootId} runtimeId={runtimeId} agentId={agentId}
    file={file.content} name={file.name || `Attachment ${index + 1}`} image={file.kind === 'image'} connected={connected}/>;
  return <>
    {valid && <BrowserDesignAttachment context={{ ...designContext, elements: designContext.elements ?? [], url: designContext.page_url, title: designContext.page_title }} connected={connected} readContext={readContext}
      screenshot={screenshot && render(screenshot, 0)}/>}
    {files?.filter(file => !valid || (file !== contextFile && file !== screenshot)).map(render)}
  </>;
}
