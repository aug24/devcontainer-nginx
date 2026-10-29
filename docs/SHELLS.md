# Shell Access Guide

The project exposes two browser-based shell entry points from the diagnostics homepage:

- Local Container Shell
- Remote Bastion Shell

Each opens a terminal in a new browser tab and connects through the app's websocket-backed PTY flow.

## Local Container Shell

The local shell is intended for interactive work inside the running proxy container itself.

Access it from the homepage action list or directly from:

```text
https://devcontainer-nginx.<base-domain>/shell-launch
```

This page opens a browser terminal that is attached to the local container environment. It is best used for quick debugging, shell access, and runtime inspection when you are already operating inside the stack.

## Remote Bastion Shell

The remote shell is intended for AWS-hosted bastion access. It resolves the correct target instance from AWS tags and then opens an SSM session to that host.

Access it from the homepage action list or directly from:

```text
https://devcontainer-nginx.<base-domain>/remote-shell-launch
```

This page opens a separate remote terminal session for a bastion-host workflow. It keeps the local container shell and remote AWS session clearly distinct so the user experience is easier to reason about.

## Idle timeout

Both shell types use websocket keepalive handling so that the terminal stays connected while you are actively using it.

The current idle timeout is set to:

```text
10 minutes
```

After 10 minutes of inactivity, the websocket is considered idle and the browser session will be disconnected. This is intentionally longer than the default short websocket timeout and is intended to avoid abrupt disconnects during slower remote work or long-lived shell sessions.

## Best practices

- Use the Local Container Shell for container-level diagnostics.
- Use the Remote Bastion Shell for AWS-targeted, bastion-host access.
- Keep both sessions in separate tabs to avoid confusion between local and remote execution contexts.
- If the shell is idle for more than 10 minutes, reconnect from the relevant launch page.

---

This page complements the main project docs and helps explain the purpose and timeout expectations of the browser shell workflows.

