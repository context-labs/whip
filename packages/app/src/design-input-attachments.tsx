import { useCallback } from 'react';
import type { Client } from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';
import type { DesignContext } from './browser-design-presentation';
import type { InputPreview } from './input-presentation';
import { BrowserDesignAttachment } from './browser-design-attachment';
import { InputAttachment } from './input-attachment';

/** Shared pending/queued presentation; identity, not a filename, groups evidence. */
export function DesignInputAttachments({ files, designContext, client, rootId, runtimeId, agentId, connected }: {
  files?: InputPreview['attachments'];
  designContext?: DeepReadonly<DesignContext> | null;
  client: Client; rootId: string; runtimeId: string; agentId: string; connected: boolean;
}) {
  const contextFile = files?.find(file => file.media_type.startsWith('text/') && file.session_id === agentId && file.id === designContext?.context_attachment_id);
  const screenshot = files?.find(file => file.media_type.startsWith('image/') && file.session_id === agentId && file.id === designContext?.screenshot_attachment_id);
  const valid = designContext && contextFile && (!designContext.screenshot_attachment_id || screenshot);
  const readContext = useCallback(async (signal: AbortSignal) => new TextDecoder('utf-8', { fatal: true }).decode(await client.session(agentId).content.readBytes(contextFile!, { maxBytes: 65536, signal })), [client, contextFile, agentId]);
  const render = (file: NonNullable<typeof files>[number], index: number) => <InputAttachment key={`${file.id}:${index}`} client={client} rootId={rootId} runtimeId={runtimeId} agentId={agentId}
    file={file} name={`Attachment ${index + 1}`} image={file.media_type.startsWith('image/')} connected={connected}/>;
  return <>
    {valid && <BrowserDesignAttachment context={{ ...designContext, elements: designContext.elements ?? [], url: designContext.page_url, title: designContext.page_title }} connected={connected} readContext={readContext}
      screenshot={screenshot && render(screenshot, 0)}/>}
    {files?.filter(file => !valid || (file !== contextFile && file !== screenshot)).map(render)}
  </>;
}
