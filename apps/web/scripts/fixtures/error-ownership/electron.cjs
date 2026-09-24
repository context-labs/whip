const { app, BrowserWindow } = require('electron');
app.setPath('userData', process.env.WHIP_ERROR_ELECTRON_DATA);
app.whenReady().then(() => {
  const window = new BrowserWindow({ width: 1440, height: 1000, title: 'Whip — Error ownership acceptance',
    webPreferences: { contextIsolation: true, nodeIntegration: false, sandbox: true } });
  window.loadURL(process.env.WHIP_ERROR_ORIGIN);
});
app.on('window-all-closed', () => app.quit());
