import {runBrowserSmoke} from './browser-smoke';
import {StrictMode, useState} from 'react';
import {createRoot} from 'react-dom/client';
import {Button, CodeBlock, Dialog, Menu, ThemeProvider, UIProvider, useTheme, themeCatalog} from '@whip/ui';
import '@whip/ui/fonts.css';
import '@whip/ui/reset.css';

function Fixture() {
  const {setTheme, theme, addTheme} = useTheme(); const [open, setOpen] = useState(false);
  const customCode = () => {
    const original = themeCatalog.find(item => item.id === 'dark')!;
    addTheme({...original, id: 'custom:csp', name: 'CSP code theme', code: {foreground: '#eeeeee', background: '#102030', tokens: {Keyword: {color: '#ffffff', background: '#203040', bold: true, italic: true, underline: true}}}});
    setTheme('custom:csp');
  };
  return <main><h1>WHIP components</h1><Button onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}>Switch theme</Button><Button onClick={customCode}>Custom code theme</Button><Button onClick={() => setOpen(true)}>Open dialog</Button><Menu trigger={<Button>Open menu</Button>} items={[{id: 'inspect', label: 'Inspect safely'}]}/><CodeBlock language="starlark" label="Starlark example" code={'def inspect(name):\n    # Read-only inspection\n    return agents.get(name="explore", limit=12)\n'}/><CodeBlock language="javascript" label="Escaped JavaScript" code={'const value = "<img src=x onerror=alert(1)>";'}/><CodeBlock language="go" code={'package main\nfunc main() { println("hello") }'}/><CodeBlock language="json" code={'{"root_id":"root","budget":12}'}/><CodeBlock language="shell" code={'printf "%s\\n" "$WHIP_HOME"'}/><CodeBlock code="Unformatted output remains exact."/><CodeBlock language="unknown" code="An unsupported language stays plain text."/><CodeBlock language="python" code={'# Large bounded output\n'.repeat(2000)} downloadAction={<Button size="sm">Read full output</Button>}/><Dialog open={open} onOpenChange={setOpen} title="Theme in a portal"><CodeBlock language="starlark" code="return True"/></Dialog></main>;
}
createRoot(document.getElementById('root')!).render(<StrictMode><ThemeProvider initialTheme="dark"><UIProvider><Fixture/></UIProvider></ThemeProvider></StrictMode>);

void runBrowserSmoke();
