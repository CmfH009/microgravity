const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext();
  const page = await context.newPage();

  console.log('Testing Google Antigravity OAuth Client ID...');
  const url = 'https://accounts.google.com/o/oauth2/auth?access_type=offline&client_id=681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com&code_challenge=ZbWIBZWZjRQjVXf6AW0Xwmxe9j8vIecY832qwxn3JYA&code_challenge_method=S256&redirect_uri=http%3A%2F%2Flocalhost%3A8080%2Fcallback&response_type=code&scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fcloud-platform+https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fuserinfo.profile+https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fuserinfo.email+openid&state=state-token';
  
  await page.goto(url);
  await page.waitForTimeout(3000);

  const title = await page.title();
  console.log('=== Page Title ===');
  console.log(title);

  const bodyText = await page.innerText('body');
  console.log('=== Body Text Snippet ===');
  console.log(bodyText.substring(0, 1000));

  await browser.close();
})();
