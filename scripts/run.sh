#!/bin/bash
# Run everything: server, wifi watchdog and the two kiosk browsers.
# Ctrl-C (or closing the terminal) stops it all.
cd "$(dirname "$0")/.."

# Height in pixels of the top screen; the bottom screen starts below it.
SCREEN_HEIGHT=${SCREEN_HEIGHT:-1080}
URL=http://localhost:9000
PROFILES=$HOME/.config/wall-chromium

trap 'exit' INT TERM
trap 'trap - TERM; kill 0' EXIT

if [ ! -x bin/wall ]; then
    echo 'bin/wall not found -- run scripts/deploy.sh first'
    exit 1
fi

BROWSER=$(command -v chromium || command -v chromium-browser)
if [ -z "$BROWSER" ]; then
    echo 'chromium not found'
    exit 1
fi

xset s off
xset -dpms
xset s noblank
unclutter -idle 1 &

# Server, restarted if it exits.
while true
do
    ./bin/wall -key backend/wall-gcloud.key -static frontend/dist
    echo 'server exited -- restarting in 5s..'
    sleep 5
done &

# Restart wifi when the internet is unreachable.
while true
do
    wget -q --spider http://google.com
    if [ $? -eq 0 ]; then
        sleep 20
        continue
    fi

    echo 'request to google.com failed -- restarting wifi..'
    nmcli radio wifi off
    sleep 5
    nmcli radio wifi on
    sleep 30
done &

until wget -q --spider $URL/; do sleep 1; done

# Each window gets its own profile dir, otherwise the second one just opens
# as a tab in the first. Dedicated profiles also skip the profile picker.
kiosk() {
    "$BROWSER" --kiosk \
        --user-data-dir="$PROFILES/$1" \
        --window-position="$2" \
        --no-first-run \
        --no-default-browser-check \
        --noerrdialogs \
        --disable-session-crashed-bubble \
        --password-store=basic \
        "$URL/$1" >/dev/null 2>&1 &
}

kiosk top 0,0
kiosk bottom 0,$SCREEN_HEIGHT

wait
