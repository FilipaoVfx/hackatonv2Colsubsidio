Output demo/secura.gif

Set Shell "bash"
Set FontSize 15
Set Width 1400
Set Height 820
Set Padding 20
Set TypingSpeed 60ms
Set Theme { "background": "#121824", "foreground": "#ffffff", "black": "#121824", "brightBlack": "#78859e", "red": "#ff5c5c", "brightRed": "#ff5c5c", "green": "#2bd576", "brightGreen": "#2bd576", "yellow": "#ffe600", "brightYellow": "#ffe600", "blue": "#4f8bff", "brightBlue": "#7fa9ff", "cursor": "#ffe600" }

Hide
Type "cd /root/guardian-ai/cli && ./bin/secura --api-url http://localhost:8099"
Enter
Sleep 500ms
Show

# Splash: capabilities checklist reveals real /api/capabilities
Sleep 3s
Enter
Sleep 500ms

# Dashboard: real health, capabilities, KPIs
Sleep 2s

# Command palette: fuzzy filter -> Pipeline
Ctrl+p
Sleep 600ms
Type "pipe"
Sleep 600ms
Enter
Sleep 500ms

# Pipeline: trigger a REAL WhatsApp conversation end-to-end
Type "w"
Sleep 6s

# Conversaciones: the call we just created, real drill-down
Shift+Tab
Sleep 800ms
Enter
Sleep 1s
Right
Sleep 800ms
Right
Sleep 1200ms
Escape
Sleep 500ms

# Knowledge: real RAG runtime + queries seen this session
Tab
Tab
Tab
Tab
Sleep 1500ms

# Analytics: real KPIs + derived conversion
Tab
Sleep 1500ms

Type "q"
Sleep 500ms
