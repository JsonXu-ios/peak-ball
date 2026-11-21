// Render the original vector mark without launching an app or HTTP service.
const fs = require('node:fs');
const path = require('node:path');
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'C:/Users/Administrator/AppData/Local/npm-cache/_npx/e41f203b7505f1fb/node_modules/playwright');
const root = path.resolve(__dirname, '..');
(async () => {
  const browser = await chromium.launch({channel:'chrome',headless:true});
  try {
    const page = await browser.newPage({viewport:{width:512,height:512},deviceScaleFactor:2});
    const svg = fs.readFileSync(path.join(root,'assets/brand/logo.svg'),'utf8');
    fs.mkdirSync(path.join(root,'assets/images'),{recursive:true});
    await page.setContent(`<style>html,body{margin:0;background:transparent}svg{display:block}</style>${svg}`);
    await page.screenshot({path:path.join(root,'assets/images/logo.png'),omitBackground:true});
  } finally { await browser.close(); }
})().catch(error=>{console.error(error);process.exitCode=1});
