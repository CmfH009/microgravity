const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch({ headless: false });
  const context = await browser.newContext();
  const page = await context.newPage();

  const url = 'https://accounts.google.com/o/oauth2/auth?access_type=offline&client_id=32555940559.apps.googleusercontent.com&code_challenge=68ifA9UzxYumJuBkWaLIK-3YwcTCEwZyA0C-ESAUkWA&code_challenge_method=S256&redirect_uri=http%3A%2F%2Flocalhost%3A8080%2Fcallback&response_type=code&scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fcloud-platform+https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fuserinfo.profile+https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fuserinfo.email+openid&state=state-token';
  
  await page.goto(url);
  console.log('Opened OAuth URL in headed Playwright browser.');
})();
