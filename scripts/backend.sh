#/bin/bash

cd ../backend/server

while true 
do
    go run server/server.go
    sleep 5
done


