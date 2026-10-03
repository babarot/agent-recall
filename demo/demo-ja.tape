# Records the Japanese demo, demo/ja/demo.gif, from the demo `make demo-ja`
# builds (see demo/README.md). It is not committed; make it when you need it.

Output demo/ja/demo.gif

Set Shell "bash"
# IBM Plex Mono, with IBM Plex Sans JP for the Japanese it does not have,
# sized for the same 115 columns and about 36 rows as demo.tape.
Set FontFamily "IBM Plex Mono, IBM Plex Sans JP"
Set FontSize 24
Set Width 1800
Set Height 1240
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms

Hide
Type 'source demo/.out/env.sh && cd "$HOME" && clear'
Enter
Show

# Somewhere in the last couple of weeks, users kept getting logged out
# and it got fixed. Look for it.
Sleep 300ms
Type "recall"
Enter
Sleep 1.5s
Down@350ms 3
Sleep 900ms
Down@300ms 2
Sleep 400ms
Up@400ms 1
Sleep 800ms

# It was in the api, and something about sessions.
Type "/"
Sleep 300ms
Type@30ms "folder:api"
Sleep 400ms
Type@90ms " セッション"
Sleep 1s
Enter
Sleep 600ms

# Two candidates. Skim the first: a cookie change, not it.
Space
Sleep 1s
Ctrl+D
Sleep 900ms

# The next one, in place: 401s that signed users out. This one?
Tab
Sleep 300ms
Type "j"
Sleep 900ms
Tab
Sleep 400ms
Ctrl+D
Sleep 1.1s

# It was about refreshing the token: search the conversation for that.
Type "/"
Sleep 300ms
Type@70ms "リフレッシュ"
Sleep 1.6s
Escape
Sleep 400ms
Escape
Sleep 500ms

# Not there, so not it. Ask Claude what is remembered.
Type "a"
Sleep 400ms
Type@55ms "リクエストが同時に飛ぶとログインが切れる件、どう直したっけ"
Sleep 300ms
Enter
Sleep 5.5s
Enter
Sleep 900ms

# Search it for the fix to be sure: this is the one.
Space
Sleep 900ms
Type "/"
Sleep 300ms
Type@60ms "singleflight"
Sleep 400ms
Enter
Sleep 1.3s
Type "n"
Sleep 3.5s
