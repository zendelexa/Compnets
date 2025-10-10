kill -9 $(lsof -ti :1234)
kill %1
cd ../сompnets
go run server.go &
sleep 3s
cd ../k6tests
k6 run script.js && kill -9 $(lsof -ti :1234)
