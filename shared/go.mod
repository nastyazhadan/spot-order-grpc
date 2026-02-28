module github.com/nastyazhadan/spot-order-grpc/shared

go 1.25.0

replace github.com/nastyazhadan/spot-order-grpc/protos => ../protos

require (
	buf.build/go/protovalidate v1.1.2
	github.com/gomodule/redigo v1.9.3
	github.com/google/uuid v1.6.0
	github.com/ilyakaznacheev/cleanenv v1.5.0
	github.com/jackc/pgx/v5 v5.8.0
	github.com/nastyazhadan/spot-order-grpc/protos v0.0.0-20260228150309-129aa47537e4
	github.com/pressly/goose/v3 v3.26.0
	github.com/sony/gobreaker/v2 v2.4.0
	go.uber.org/zap v1.27.1
	google.golang.org/grpc v1.79.1
	google.golang.org/protobuf v1.36.11
)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.11-20260209202127-80ab13bee0bf.1 // indirect
	cel.dev/expr v0.25.1 // indirect
	github.com/BurntSushi/toml v1.2.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/google/cel-go v0.27.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/sdk v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/exp v0.0.0-20260212183809-81e46e3db34a // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260217215200-42d3e9bedb6d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260223185530-2f722ef697dc // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	olympos.io/encoding/edn v0.0.0-20201019073823-d3554ca0b0a3 // indirect
)
