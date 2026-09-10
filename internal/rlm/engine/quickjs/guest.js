(() => {
  const host = globalThis.__whipSubmit;
  delete globalThis.__whipSubmit;
  const parse = JSON.parse, quote = JSON.stringify;
  const ownKeys = Reflect.ownKeys, descriptor = Object.getOwnPropertyDescriptor;
  const proto = Object.getPrototypeOf, objectProto = Object.prototype, arrayProto = Array.prototype;
  const isArray = Array.isArray, finite = Number.isFinite;
  const create = Object.create, freeze = Object.freeze, define = Object.defineProperty;
  const NativeError = Error, NativePromise = Promise, NativeSet = Set;
  const call = Function.prototype.call.bind(Function.prototype.call);
  const charCodeAt = String.prototype.charCodeAt, textSlice = String.prototype.slice, textIncludes = String.prototype.includes, same = Object.is;
  const setHas = Set.prototype.has, setAdd = Set.prototype.add, setDelete = Set.prototype.delete;
  const promiseReject = NativePromise.reject.bind(NativePromise);
  const nativeBigInt = BigInt, nativeNumber = Number, safeInteger = Number.isSafeInteger, integer = Number.isInteger;
  const numberToken = new WeakMap(), tokenGet = WeakMap.prototype.get.bind(numberToken), tokenSet = WeakMap.prototype.set.bind(numberToken);
  const rejected = new NativeSet();
  const rejectedClear = Set.prototype.clear.bind(rejected);
  const setSize = Object.getOwnPropertyDescriptor(Set.prototype, 'size').get;
  // Reflection cannot passively inspect Proxy traps; this initial host-payload
  // profile disables their construction before any user source is evaluated.
  define(globalThis, 'Proxy', {value:undefined, writable:false, configurable:false});
  class ExactNumber {
    constructor(text) { tokenSet(this, text); freeze(this); }
    valueOf() { return nativeNumber(tokenGet(this)); }
    toString() { return tokenGet(this); }
  }
  function decode(text) {
    if (typeof text !== 'string') throw fault('E_JSON', 'JSON input must be a string');
    let i = 0, depth = 0;
    function space() { while (text[i] === ' ' || text[i] === '\n' || text[i] === '\r' || text[i] === '\t') i++; }
    function string() {
      const start = i++;
      while (i < text.length) { const c = text[i++]; if (c === '"') return parse(call(textSlice,text,start,i)); if (c === '\\') i++; }
      throw fault('E_JSON', 'unterminated JSON string');
    }
    function value() {
      space(); if (++depth > 64) throw fault('E_JSON', 'JSON nesting limit');
      let result;
      if (text[i] === '"') result = string();
      else if (text[i] === '{' || text[i] === '[') {
        const array = text[i++] === '[', close = array ? ']' : '}'; result = array ? [] : create(null); space();
        if (text[i] !== close) for (;;) {
          let key;
          if (!array) { if (text[i] !== '"') throw fault('E_JSON','JSON object key'); key = string(); space(); if(text[i++] !== ':') throw fault('E_JSON','JSON colon'); }
          const item = value();
          if (array) define(result,result.length,{value:item,writable:true,enumerable:true,configurable:true}); else { if (descriptor(result,key)) throw fault('E_JSON','duplicate JSON key'); define(result,key,{value:item,writable:true,enumerable:true,configurable:true}); }
          space(); if(text[i] === close) break; if(text[i++] !== ',') throw fault('E_JSON','JSON comma'); space();
        }
        i++;
      } else {
        const start = i;
        while(i < text.length && text[i] !== ',' && text[i] !== ']' && text[i] !== '}' && text[i] !== ' ' && text[i] !== '\n' && text[i] !== '\r' && text[i] !== '\t') i++;
        const raw = call(textSlice,text,start,i), parsed = parse(raw);
        if(typeof parsed !== 'number') result = parsed;
        else if (call(textIncludes,raw,'.') || call(textIncludes,raw,'e') || call(textIncludes,raw,'E') || raw === '-0') result = new ExactNumber(raw);
        else result = safeInteger(parsed) ? parsed : nativeBigInt(raw);
      }
      depth--; return result;
    }
    const result = value(); space(); if(i !== text.length) throw fault('E_JSON','trailing JSON input'); return result;
  }

  const map = new Map();
  const get = Map.prototype.get.bind(map), set = Map.prototype.set.bind(map);
  const del = Map.prototype.delete.bind(map), each = Map.prototype.forEach.bind(map);
  let active = false, output = '', hasValue = false;
  let count = 0, cellID = '', prefix = '', ordinal = 0, status = 'idle', value = 'null', error = 'null';
  const limits = __WHIP_LIMITS__;
  const tools = __WHIP_TOOLS__;
  function fault(code, message) { const e = new NativeError(message); define(e, 'code', {value:code}); return e; }
  function utf8(s) {
    let n = 0;
    for (let i = 0; i < s.length; i++) {
      const c = call(charCodeAt, s, i);
      if (c < 128) n++; else if (c < 2048) n += 2;
      else if (c >= 0xd800 && c <= 0xdbff && i + 1 < s.length && call(charCodeAt,s,i+1) >= 0xdc00 && call(charCodeAt,s,i+1) <= 0xdfff) { n += 4; i++; }
      else n += 3;
    }
    return n;
  }
  function validString(s) {
    for (let i=0;i<s.length;i++) {
      const c=call(charCodeAt,s,i);
      if (c>=0xd800 && c<=0xdbff) {
        const next=call(charCodeAt,s,++i);
        if (!(next>=0xdc00 && next<=0xdfff)) throw fault('E_JSON','host strings require valid Unicode');
      } else if (c>=0xdc00 && c<=0xdfff) throw fault('E_JSON','host strings require valid Unicode');
    }
  }
  function encode(input, max, preview = false) {
    const seen = new NativeSet();
    let nodes = 0, bytes = 0;
    function piece(s) { bytes += utf8(s); if (bytes > max) throw fault('E_LIMIT', 'JSON byte limit'); return s; }
    function visit(v, depth) {
      if (++nodes > max || depth > 64) throw fault('E_JSON', 'JSON complexity limit');
      if (typeof v === 'string') validString(v);
      if (v === null || typeof v === 'string' || typeof v === 'boolean') return piece(quote(v));
      if (typeof v === 'bigint') return piece(preview ? '{"type":"bigint","value":' + quote('' + v) + '}' : '' + v);
      if (typeof v === 'object' && v !== null && tokenGet(v) !== undefined) return piece(preview ? '{"type":"number","value":' + quote(tokenGet(v)) + '}' : tokenGet(v));
      if (typeof v === 'number' && finite(v)) {
        if (integer(v) && !safeInteger(v)) throw fault('E_JSON', 'unsafe integer Number; construct BigInt from a decimal string');
        return piece(same(v,-0) ? '-0.0' : quote(v));
      }
      if (preview && typeof v !== 'object') return piece('{"type":' + quote(typeof v) + '}');
      if (typeof v !== 'object') throw fault('E_JSON', 'only finite JSON trees are supported');
      if (call(setHas, seen, v)) { if(preview) return piece('{"type":"cycle"}'); throw fault('E_JSON', 'cyclic JSON value'); }
      const array = isArray(v), p = proto(v);
      if (p !== objectProto && p !== null && !(array && p === arrayProto)) throw fault('E_JSON', 'non-JSON object');
      call(setAdd, seen, v);
      const names = ownKeys(v);
      let text = piece(array ? '[' : '{'), elements = 0;
      if (array) {
        const len = descriptor(v, 'length').value;
        if (len > max) throw fault('E_JSON', 'JSON array limit');
        for (let i = 0; i < len; i++) {
          const d = descriptor(v, i);
          if (!d || !descriptor(d, 'value')) throw fault('E_JSON', 'sparse/accessor JSON array');
          if (i) text += piece(',');
          text += visit(d.value, depth + 1);
        }
      }
      for (let i = 0; i < names.length; i++) {
        const key = names[i], d = descriptor(v, key);
        if (typeof key !== 'string') throw fault('E_JSON', 'symbol JSON key');
        validString(key);
        if (!descriptor(d, 'value')) throw fault('E_JSON', 'JSON accessors are unsupported');
        if (!array && d.enumerable) {
          if (elements++) text += piece(',');
          text += piece(quote(key) + ':') + visit(d.value, depth + 1);
        }
      }
      call(setDelete, seen, v);
      return text + piece(array ? ']' : '}');
    }
    return visit(input, 0);
  }
  function remoteError(e) {
    let code = 'E_GUEST', message = 'guest rejected';
    try {
      if (typeof e === 'string') message = e;
      else if (e && typeof e === 'object') {
        const c = descriptor(e, 'code'), m = descriptor(e, 'message');
        if (c && typeof c.value === 'string') code = c.value;
        if (m && typeof m.value === 'string') message = m.value;
      }
      const result = '{"code":' + quote(code) + ',"message":' + quote(message) + '}';
      if (utf8(result) <= limits.MaxOutputBytes) return result;
    } catch (_) {}
    return '{"code":"E_LIMIT","message":"guest error byte limit or accessor"}';
  }
  function pendingJSON() {
    let text = '[', first = true;
    each((_, id) => { if (!first) text += ','; first = false; text += quote(id); });
    return text + ']';
  }
  function submit(tool, args) {
    if (!active) return promiseReject(fault('E_CELL', 'tool call outside active cell'));
    return new NativePromise((resolve, reject) => {
      let id;
      try {
        if (args === null || typeof args !== 'object' || isArray(args)) throw fault('E_JSON', 'tool arguments must be a JSON object');
        const text = encode(args, limits.MaxRequestBytes);
        id = prefix + (++ordinal);
        const waiter = create(null); waiter.resolve = resolve; waiter.reject = reject;
        set(id, waiter); count++;
        const result = host(id, tool, text);
        if (result) throw fault(result, 'host refused tool request: ' + result);
      } catch (e) { if (id && del(id)) count--; reject(e); }
    });
  }
  const modules = create(null);
  for (const tool of tools) {
    const [module, method] = tool.split('.');
    if (!modules[module]) modules[module] = create(null);
    define(modules[module], method, {value:(args = {}) => submit(tool, args), enumerable:true});
  }
  for (const module of ownKeys(modules)) define(globalThis, module, {value:freeze(modules[module]), writable:false, configurable:false, enumerable:true});
  function print(...values) {
    let line = '';
    for (let i=0;i<values.length;i++) { const v=values[i]; if (line) line += ' '; line += typeof v === 'string' ? v : encode(v, limits.MaxOutputBytes, true); }
    if (utf8(output) + utf8(line) + 1 > limits.MaxOutputBytes) throw fault('E_LIMIT', 'cell output limit exceeded');
    output += line + '\n';
  }
  define(globalThis, 'print', {value:print, writable:false, configurable:false});
  define(globalThis, 'console', {value:freeze({log:print,info:print,warn:print,error:print}), writable:false, configurable:false});
  define(globalThis, 'json', {value:freeze({encode:v => encode(v,limits.MaxResultBytes),decode}), writable:false, configurable:false});
  const control = create(null);
  control.begin = (id, scope) => {
    if (active || count) throw fault('E_BUSY', 'previous cell is not settled');
    cellID = id; prefix = scope; ordinal = 0; status = 'running'; value = 'null'; error = 'null'; active = true; output = ''; hasValue = false; rejectedClear();
  };
  control.fulfilled = envelope => {
    try { hasValue = envelope.value !== undefined; value = hasValue ? encode(envelope.value, limits.MaxOutputBytes, true) : 'null'; status = 'complete'; }
    catch (e) { error = remoteError(e); status = 'failed'; }
  };
  control.rejected = e => { error = remoteError(e); status = 'failed'; };
  control.deliver = text => {
    const out = decode(text), waiter = get(out.id);
    if (!waiter) return false;
    del(out.id); count--;
    if (out.ok) waiter.resolve(out.value);
    else waiter.reject(fault(out.error.code, out.error.message));
    return true;
  };
  control.rejectionAdd = promise => { call(setAdd,rejected,promise); };
  control.rejectionDelete = promise => { call(setDelete,rejected,promise); };
  control.finish = () => { active = false; if (call(setSize,rejected) > 0 && status !== 'failed') { status='failed'; error='{"code":"E_UNHANDLED_REJECTION","message":"unhandled Promise rejection"}'; } };
  control.inspect = () => '{"cellId":' + quote(cellID) + ',"status":' + quote(status) + ',"hasValue":' + hasValue + ',"output":' + quote(output) + ',"value":' + value + ',"error":' + error + ',"pending":' + pendingJSON() + '}';
  control.identity = () => '{"bridge":1,"prefix":' + quote(prefix) + ',"ordinal":' + ordinal + ',"cellId":' + quote(cellID) + ',"pending":' + pendingJSON() + '}';
  return freeze(control);
})()
