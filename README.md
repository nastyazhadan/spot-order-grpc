go run ./orderService/cmd/order/main.go -env .env
go run ./spotService/cmd/spot/main.go -env .env

// Проверить порт 5432
netstat -ano | findstr :5432

tasklist /FI "PID eq 6900"
tasklist /FI "PID eq 19900"

taskkill /PID 8352 /F
