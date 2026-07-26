Output demo/secura.gif

Set Shell "bash"
Set FontSize 15
Set Width 1400
Set Height 820
Set Padding 20
Set TypingSpeed 120ms
Set Theme { "background": "#121824", "foreground": "#ffffff", "black": "#121824", "brightBlack": "#78859e", "red": "#ff5c5c", "brightRed": "#ff5c5c", "green": "#2bd576", "brightGreen": "#2bd576", "yellow": "#ffe600", "brightYellow": "#ffe600", "blue": "#4f8bff", "brightBlue": "#7fa9ff", "cursor": "#ffe600" }

Hide
Type "cd /root/guardian-ai/cli && ./bin/secura --api-url http://localhost:8099"
Enter
Sleep 1000ms
Show

# Splash: capabilities checklist reveals real /api/capabilities
Sleep 6s
Enter
Sleep 1000ms

# Dashboard: real health, capabilities, KPIs
Sleep 4s

# Command palette: fuzzy filter -> Pipeline
Ctrl+p
Sleep 1200ms
Type "pipe"
Sleep 1200ms
Enter
Sleep 1000ms

# Pipeline: trigger a REAL WhatsApp conversation end-to-end
Type "w"
Sleep 12s

# Conversaciones: the call we just created, real drill-down
Shift+Tab
Sleep 1600ms
Enter
Sleep 2s
Right
Sleep 1600ms
Right
Sleep 2400ms
Escape
Sleep 1000ms

# Knowledge: real RAG runtime + queries seen this session
Tab
Tab
Tab
Tab
Sleep 3000ms

# Analytics: real KPIs + derived conversion
Tab
Sleep 3000ms

Type "q"
Sleep 1000ms
