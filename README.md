go run ./orderService/cmd/o/main.go -env .env
go run ./spotService/cmd/spot/main.go -env .env

// Проверить порт 5432
netstat -ano | findstr :5432

tasklist /FI "PID eq 6900"
tasklist /FI "PID eq 19900"

taskkill /PID 8352 /F

git filter-repo --path .env --invert-paths
