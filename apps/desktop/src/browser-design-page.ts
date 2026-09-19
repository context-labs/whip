/** Executed only in a main-created isolated world. Never reads framework props or input values. */
export const readDesignElement = `function () {
  if (!(this instanceof Element) || !this.isConnected) return null;
  const root = this.getRootNode();
  if (root instanceof ShadowRoot && root.mode === 'closed') return {unsupported:'Closed shadow content is not available. Select its host instead.'};
  const element=this;
  const rect=element.getBoundingClientRect();
  const clean=value=>String(value||'').replace(/[\u0000-\u001f]/g,' ').slice(0,160);
  const attributes={};
  for(const name of ['id','class','role','aria-label','aria-describedby','type','alt']) {
    const value=element.getAttribute(name); if(value) attributes[name]=clean(value);
  }
  const style=getComputedStyle(element);
  const styles={};
  for(const name of ['display','position','color','background-color','font-family','font-size','font-weight','line-height','padding','margin','gap','border-radius','width','height','text-align','align-items','justify-content']) styles[name]=clean(style.getPropertyValue(name));
  let text='';let visited=0;
  const walker=document.createTreeWalker(element,NodeFilter.SHOW_ALL);
  while(text.length<512 && visited++<128) {
    const node=walker.nextNode();if(!node)break;if(node.nodeType!==Node.TEXT_NODE)continue;const parent=node.parentElement;
    if(!parent||parent.closest('script,style,template,[hidden],input,textarea,select,[contenteditable]'))continue;
    const cs=getComputedStyle(parent);if(cs.display==='none'||cs.visibility==='hidden'||!parent.checkVisibility({checkOpacity:true,checkVisibilityCSS:true}))continue;
    text+=(node.nodeValue||'').trim().slice(0,96)+' ';
  }
  const tag=element.localName;
  const role=element.getAttribute('role')||({button:'button',a:element.hasAttribute('href')?'link':'',input:'input',img:'image',select:'combobox',textarea:'textbox'}[tag]||tag);
  const name=clean(element.getAttribute('aria-label')||element.getAttribute('alt')||text);
  const path=[];let item=element;
  while(item && path.length<8) {
    let part=item.localName;
    if(item.id){part+='#'+CSS.escape(item.id).slice(0,128);path.unshift(part);break;}
    if(item.parentElement){let index=1,scanned=0;for(let sibling=item.previousElementSibling;sibling;sibling=sibling.previousElementSibling){if(++scanned>256){index=0;break;}if(sibling.localName===item.localName)index++;}if(index)part+=':nth-of-type('+index+')';}
    path.unshift(part);item=item.parentElement;
  }
  return {tag,role,name,text:text.trim().slice(0,512),attributes,styles,selector:path.join(' > '),frame:tag==='iframe'?'Frame container; inner content not selected':undefined,bounds:{x:rect.x,y:rect.y,width:rect.width,height:rect.height}};
}`;
