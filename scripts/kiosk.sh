#/bin/bash

xset s off
xset -dpms
xset s noblank
unclutter -idle 1 &

while true 
do
    wget -q --spider http://google.com
    if [ $? -eq 0 ]; then
        sleep 20
        continue
    fi

    echo 'request to google.com failed -- restarting wifi..'
    echo 'turning off..'
    nmcli radio wifi off
    echo 'waiting 5 sec..'
    sleep 5
    echo 'turning back on..'
    nmcli radio wifi on
    echo 'waiting 30s...'
    sleep 30
done


