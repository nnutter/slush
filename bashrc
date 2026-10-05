# Slush's Bash startup: read login profiles before restoring shim priority.
# Bash --rcfile runs this after Bash's system interactive initialization.
_slush_token=$SLUSH_TOKEN
_slush_port=$SLUSH_PORT
if [ -r /etc/profile ]; then . /etc/profile; fi
for _slush_profile in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
    if [ -r "$_slush_profile" ]; then
        . "$_slush_profile"
        break
    fi
done
export SLUSH=1 SLUSH_TOKEN=$_slush_token SLUSH_PORT=$_slush_port BROWSER=slush-open
export PATH="${XDG_CACHE_HOME:-$HOME/.cache}/slush/bin:$PATH"
unset _slush_token _slush_port _slush_profile
