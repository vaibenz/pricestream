REDIS_URL ?= redis://localhost:6379/0
export REDIS_URL

.PHONY: up up-sim down test load app

up:
	docker compose up --build -d

up-sim:
	SOURCE=sim docker compose up --build -d

down:
	docker compose down

test:
	go test -race -count=1 ./...
	cd app && npm test && npm run typecheck

# Load test against a running gateway; run on the same host as the ingestor.
# All clients share one IP, so start the gateway with MAX_CONNS_PER_IP=0.
load:
	go run ./cmd/loadtest -url ws://localhost:8080/ws -clients 1000 -duration 20s

app:
	cd app && npx expo start
