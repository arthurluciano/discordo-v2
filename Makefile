COMPOSE := docker compose -f deploy/docker-compose.yml
DB      := postgres://discordo:discordo@localhost:54329
S3      := S3_ENDPOINT=localhost:59000 S3_ACCESS_KEY=discordo S3_SECRET_KEY=discordo-secret

.PHONY: db run web desktop test

db: ## start Postgres and MinIO, and make sure the test database exists
	$(COMPOSE) up -d --wait
	$(COMPOSE) exec -T postgres createdb -U discordo discordo_test 2>/dev/null || true

run: db ## needs DISCORD_CLIENT_ID and DISCORD_CLIENT_SECRET in the environment
	cd apps/server && DATABASE_URL=$(DB)/discordo $(S3) go run ./cmd/discordo

web: ## the web app with hot reload on :5173, proxying to `make run`
	cd apps/web && npm install && npm run dev

desktop: ## the native app (go-native-ui), against `make run`; git submodule update --init first
	cd apps/desktop && CGO_ENABLED=0 go run .

# -p 1: the packages share one test database
test: db
	cd apps/server && go vet ./... && TEST_DATABASE_URL=$(DB)/discordo_test TEST_S3_ENDPOINT=localhost:59000 go test -race -p 1 ./...
