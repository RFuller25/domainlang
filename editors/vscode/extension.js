// Domain VS Code extension: TextMate grammar (declared in package.json) plus a
// thin language-server client that launches `domain lsp` and speaks LSP over
// stdio. The server provides diagnostics, hover (primitive documentation and
// pipeline types), completion, go-to-Shikigami, and quick fixes — the same
// engine `domain expansion: lint` uses.
//
// This file is plain CommonJS: no TypeScript build step. Its only runtime
// dependency is `vscode-languageclient`; run `npm install` in this folder
// before packaging or launching the extension.

const { workspace, window, commands } = require("vscode");
const { LanguageClient, TransportKind } = require("vscode-languageclient/node");

/** @type {import('vscode-languageclient/node').LanguageClient | undefined} */
let client;

// buildClient constructs a client that runs `<domain.server.path> lsp`.
function buildClient() {
  const configured = workspace.getConfiguration("domain").get("server.path");
  const command = configured && configured.trim() !== "" ? configured : "domain";

  const executable = { command, args: ["lsp"], transport: TransportKind.stdio };
  const serverOptions = { run: executable, debug: executable };

  const clientOptions = {
    documentSelector: [{ scheme: "file", language: "domain" }],
    // Re-sync when the user edits any `domain.*` setting (e.g. the server path).
    synchronize: { configurationSection: "domain" },
  };

  return new LanguageClient("domain", "Domain Language Server", serverOptions, clientOptions);
}

async function startClient() {
  client = buildClient();
  try {
    await client.start();
  } catch (err) {
    const path = workspace.getConfiguration("domain").get("server.path") || "domain";
    window.showErrorMessage(
      `Domain: couldn't start the language server ("${path} lsp"). ` +
        `Make sure the domain binary is installed and on PATH, or set "domain.server.path". (${err})`
    );
  }
}

function activate(context) {
  startClient();

  context.subscriptions.push(
    commands.registerCommand("domain.restartServer", async () => {
      if (client) {
        await client.stop();
      }
      await startClient();
      window.showInformationMessage("Domain language server restarted.");
    })
  );
}

function deactivate() {
  return client ? client.stop() : undefined;
}

module.exports = { activate, deactivate };
