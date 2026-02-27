# headroom demo
Output demo.gif
Set Shell bash
Set FontSize 14
Set Width 1200
Set Height 900
Set Padding 16
Set Theme "Catppuccin Mocha"

# Clean prompt — suppress rc file noise
Hide
Type "export PS1='$ '"
Enter
Sleep 200ms
Type "clear"
Enter
Sleep 200ms
Show

# Show the terminal, type the command, launch
Type "./headroom --demo"
Sleep 500ms
Enter

# Loading animation + bar sweep + full demo sequence
Sleep 16s

# Hide before the app quits and drops back to shell
Hide
Sleep 2s
