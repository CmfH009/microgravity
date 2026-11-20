const vscode = require('vscode');

function activate(context) {
  // Helper to send command to terminal
  function runInTerminal(name, command) {
    let term = vscode.window.terminals.find(t => t.name === name);
    if (!term) {
      term = vscode.window.createTerminal({
        name: name,
        cwd: "A:\\projects"
      });
    }
    term.show();
    term.sendText(command);
  }

  // 1. Register Action Menu QuickPick
  context.subscriptions.push(
    vscode.commands.registerCommand('microgravity.menu', async () => {
      const items = [
        {
          label: "$(comment-discussion) 💬 Interactive Agent Chat",
          description: "Launch interactive terminal AI session",
          command: 'microgravity.chat'
        },
        {
          label: "$(tools) 🔧 Autonomous Test Repair (mg fix)",
          description: "Diagnose, repair and verify failing test suites",
          command: 'microgravity.fix'
        },
        {
          label: "$(play) 🏃 Run Autonomous Coding Task",
          description: "Execute a prompt with full tool suite & doubt loop",
          command: 'microgravity.run'
        },
        {
          label: "$(organization) 🐝 Parallel Worker Swarm",
          description: "Dispatch multi-agent swarm array across tasks",
          command: 'microgravity.swarm'
        },
        {
          label: "$(shield) 🛡️ Codebase Audit & Security Hygiene",
          description: "Audit workspace standards and test hygiene",
          command: 'microgravity.audit'
        },
        {
          label: "$(graph) 📊 Visualize Dependency & Skill Graph",
          description: "Render ASCII tree of dependencies and skills",
          command: 'microgravity.graph'
        },
        {
          label: "$(dashboard) 🌌 Open Orbit Web Cockpit",
          description: "Open Orbit Mission Control HUD (port 8765)",
          command: 'microgravity.cockpit'
        }
      ];

      const selected = await vscode.window.showQuickPick(items, {
        placeHolder: "Select a Microgravity Autonomous Action"
      });

      if (selected && selected.command) {
        vscode.commands.executeCommand(selected.command);
      }
    })
  );

  // 2. Chat Terminal
  context.subscriptions.push(
    vscode.commands.registerCommand('microgravity.chat', () => {
      runInTerminal("🌌 Microgravity Chat", "mg chat");
    })
  );

  // 3. Fix Tests
  context.subscriptions.push(
    vscode.commands.registerCommand('microgravity.fix', () => {
      runInTerminal("🌌 Microgravity Fix", "mg fix -a 5 -c");
    })
  );

  // 4. Run Task Prompt
  context.subscriptions.push(
    vscode.commands.registerCommand('microgravity.run', async () => {
      const prompt = await vscode.window.showInputBox({
        prompt: "Enter the autonomous task or feature request for Microgravity:",
        placeHolder: "e.g. Audit security in deadfall, add unit tests, and verify build"
      });
      if (prompt) {
        runInTerminal("🌌 Microgravity Task", `mg run "${prompt.replace(/"/g, '\\"')}"`);
      }
    })
  );

  // 5. Swarm Workers
  context.subscriptions.push(
    vscode.commands.registerCommand('microgravity.swarm', async () => {
      const prompt = await vscode.window.showInputBox({
        prompt: "Enter parallel task goals separated by semicolons:",
        placeHolder: "Audit security; Run tests; Verify build"
      });
      if (prompt) {
        runInTerminal("🌌 Microgravity Swarm", `mg swarm "${prompt.replace(/"/g, '\\"')}"`);
      }
    })
  );

  // 6. Audit
  context.subscriptions.push(
    vscode.commands.registerCommand('microgravity.audit', () => {
      runInTerminal("🌌 Microgravity Audit", "mg audit --fix");
    })
  );

  // 7. Graph
  context.subscriptions.push(
    vscode.commands.registerCommand('microgravity.graph', () => {
      runInTerminal("🌌 Microgravity Graph", "mg graph");
    })
  );

  // 8. Cockpit Browser Link
  context.subscriptions.push(
    vscode.commands.registerCommand('microgravity.cockpit', () => {
      vscode.env.openExternal(vscode.Uri.parse("http://localhost:8765"));
    })
  );

  // 9. Status Bar Item
  const statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  statusBar.text = "$(hubot) 🌌 Microgravity AI";
  statusBar.tooltip = "Click to open Microgravity Autonomous Agent Actions";
  statusBar.command = "microgravity.menu";
  statusBar.show();
  context.subscriptions.push(statusBar);

  // 10. Webview Provider for Agent HUD Sidebar
  const hudProvider = {
    resolveWebviewView(webviewView) {
      webviewView.webview.options = { enableScripts: true };
      webviewView.webview.html = getHudHtml();

      webviewView.webview.onDidReceiveMessage(message => {
        switch (message.type) {
          case 'runCommand':
            vscode.commands.executeCommand(message.command);
            break;
          case 'dispatchPrompt':
            if (message.prompt) {
              runInTerminal("🌌 Microgravity Task", `mg run "${message.prompt.replace(/"/g, '\\"')}"`);
            }
            break;
        }
      });
    }
  };

  context.subscriptions.push(
    vscode.window.registerWebviewViewProvider('microgravity.agentHud', hudProvider)
  );
}

function getHudHtml() {
  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <style>
    body {
      font-family: var(--vscode-font-family, -apple-system, sans-serif);
      padding: 12px;
      color: var(--vscode-foreground);
      background-color: var(--vscode-editor-background);
      margin: 0;
    }
    .badge-status {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 3px 8px;
      border-radius: 9999px;
      background: rgba(34, 197, 94, 0.15);
      border: 1px solid rgba(34, 197, 94, 0.4);
      color: #4ade80;
      font-size: 11px;
      font-weight: 600;
      margin-bottom: 12px;
    }
    .pulse-dot {
      width: 7px;
      height: 7px;
      border-radius: 50%;
      background: #22c55e;
      box-shadow: 0 0 6px #22c55e;
    }
    .card {
      background: var(--vscode-sideBar-background, #18181b);
      border: 1px solid var(--vscode-panel-border, rgba(255,255,255,0.1));
      border-radius: 8px;
      padding: 10px;
      margin-bottom: 12px;
    }
    .card-title {
      font-size: 11px;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--vscode-descriptionForeground);
      margin-bottom: 8px;
      font-weight: 700;
    }
    .action-grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 6px;
      margin-bottom: 12px;
    }
    button.btn {
      background: var(--vscode-button-background);
      color: var(--vscode-button-foreground);
      border: none;
      padding: 8px;
      border-radius: 6px;
      font-size: 11px;
      font-weight: 600;
      cursor: pointer;
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 6px;
      transition: opacity 0.15s;
    }
    button.btn:hover {
      background: var(--vscode-button-hoverBackground);
    }
    button.btn-secondary {
      background: var(--vscode-button-secondaryBackground, rgba(255,255,255,0.08));
      color: var(--vscode-button-secondaryForeground, #fff);
    }
    textarea.prompt-input {
      width: 100%;
      box-sizing: border-box;
      background: var(--vscode-input-background);
      color: var(--vscode-input-foreground);
      border: 1px solid var(--vscode-input-border, rgba(255,255,255,0.15));
      border-radius: 6px;
      padding: 8px;
      font-family: inherit;
      font-size: 12px;
      resize: vertical;
      min-height: 60px;
      margin-bottom: 8px;
    }
    textarea.prompt-input:focus {
      outline: 1px solid var(--vscode-focusBorder);
    }
    .info-row {
      display: flex;
      justify-content: space-between;
      font-size: 11px;
      padding: 3px 0;
      border-bottom: 1px solid rgba(255,255,255,0.05);
    }
    .info-row:last-child {
      border-bottom: none;
    }
    .info-label {
      color: var(--vscode-descriptionForeground);
    }
    .info-val {
      font-weight: 600;
      color: #38bdf8;
    }
  </style>
</head>
<body>
  <div class="badge-status">
    <div class="pulse-dot"></div>
    MICROGRAVITY AGENT ACTIVE
  </div>

  <div class="card">
    <div class="card-title">⚡ Quick Dispatch</div>
    <div class="action-grid">
      <button class="btn" onclick="sendCmd('microgravity.chat')">💬 Chat</button>
      <button class="btn" onclick="sendCmd('microgravity.fix')">🔧 Fix Tests</button>
      <button class="btn btn-secondary" onclick="sendCmd('microgravity.audit')">🛡️ Audit</button>
      <button class="btn btn-secondary" onclick="sendCmd('microgravity.graph')">📊 Graph</button>
    </div>
  </div>

  <div class="card">
    <div class="card-title">🎯 Autonomous Prompt</div>
    <textarea id="prompt" class="prompt-input" placeholder="Instruct Microgravity to inspect, code, test or refactor..."></textarea>
    <button class="btn" style="width: 100%; justify-content: center;" onclick="dispatch()">🚀 Dispatch Agent</button>
  </div>

  <div class="card">
    <div class="card-title">🛰️ Model & Mesh Endpoints</div>
    <div class="info-row">
      <span class="info-label">Local Model</span>
      <span class="info-val">gemma2:2b (Ollama :11434)</span>
    </div>
    <div class="info-row">
      <span class="info-label">Cloud Model</span>
      <span class="info-val">gemini-2.5-flash / gemma-2-9b</span>
    </div>
    <div class="info-row">
      <span class="info-label">API Serve Port</span>
      <span class="info-val">:8082</span>
    </div>
    <div class="info-row">
      <span class="info-label">Orbit Cockpit</span>
      <span class="info-val">:8765</span>
    </div>
  </div>

  <button class="btn btn-secondary" style="width: 100%; margin-top: 4px;" onclick="sendCmd('microgravity.cockpit')">🌌 Open Orbit Mission Control</button>

  <script>
    const vscode = acquireVsCodeApi();
    function sendCmd(cmd) {
      vscode.postMessage({ type: 'runCommand', command: cmd });
    }
    function dispatch() {
      const txt = document.getElementById('prompt').value.trim();
      if (txt) {
        vscode.postMessage({ type: 'dispatchPrompt', prompt: txt });
        document.getElementById('prompt').value = '';
      }
    }
  </script>
</body>
</html>`;
}

function deactivate() {}

module.exports = {
  activate,
  deactivate
};
