#!/usr/bin/env bash
# Serves the Secura TUI over the browser via ttyd.
#
# The command is the secura binary directly, never a shell: there is no escape
# to bash, and pressing `q` simply ends the session. ttyd forks one process per
# client, so each juror gets an isolated TUI.
#
# --read-only is not optional here. This session is public, and the Settings /
# Prompt modules can publish and roll back the production agent config.
set -euo pipefail

# Port 7682: 7681 is already held by an unrelated ttyd on this host.
exec ttyd \
  --port 7682 \
  --interface 127.0.0.1 \
  --max-clients 10 \
  --writable \
  --terminal-type xterm-256color \
  -t fontSize=15 \
  -t 'titleFixed=Secura CLI — Guardian AI Operations Center' \
  -t 'theme={"background":"#121824","foreground":"#ffffff","cursor":"#ffe600","black":"#121824","brightBlack":"#78859e","red":"#ff5c5c","brightRed":"#ff5c5c","green":"#2bd576","brightGreen":"#2bd576","yellow":"#ffe600","brightYellow":"#ffe600","blue":"#4f8bff","brightBlue":"#7fa9ff"}' \
  /root/guardian-ai/cli/bin/secura --read-only --api-url http://localhost:8099
