# Level 1 — Small launch

## Story and goal

A new video site has one popular video and a small audience. A viewer can see its like count, like it once, and unlike it. We run one frontend, one backend, and one PostgreSQL database locally. This is the baseline we will load test before changing the design.

The viewer is identified by a UUID stored in the browser. This is a local demo identity, not an account or authentication system. One viewer can have at most one active like per video.

## Request flows

```text
Browser (React)
    │ HTTP
    ▼
Backend (Go + Gin, one instance)
    │ SQL
    ▼
PostgreSQL

Backend /metrics ──scraped by──▶ Prometheus ──▶ Grafana
Backend traces ──OTLP──▶ OpenTelemetry Collector ──▶ Tempo ──▶ Grafana
k6 ──HTTP load──▶ Backend
```

1. **Open video:** `GET /api/videos/{videoId}` with `X-Demo-Viewer-Id`. Backend uses GORM to read the video, count its rows in `video_likes`, check whether this viewer has a row, and return `title`, `likeCount`, and `likedByViewer`.
2. **Like:** `PUT /api/videos/{videoId}/like` with the same viewer ID. Backend inserts `(video_id, viewer_id)` into `video_likes`; the primary key makes a retry harmless. It returns the latest count and viewer state.
3. **Unlike:** `DELETE /api/videos/{videoId}/like`. Backend deletes that viewer's row, if present, and returns the latest count and viewer state.

PostgreSQL is the source of truth. There is no cache or separate counter value in Level 1. Counting rows on every read is intentionally simple; we will observe how it behaves as traffic grows.

## Database diagram

Paste [schema.dbml](schema.dbml) into [dbdiagram.io](https://dbdiagram.io/) to view the diagram. `video_likes` records current likes, not a history of every click. The matching versioned migration scripts are [up](migrations/000001_initial_schema.up.sql) and [down](migrations/000001_initial_schema.down.sql).

## Tech stack

| Part | Choice | Purpose |
| --- | --- | --- |
| Frontend | React, TypeScript, Vite | One video card, count, Like/Unlike button |
| Backend | Go, Gin | HTTP routes and middleware |
| ORM | GORM with its PostgreSQL driver | Read and write video and like records |
| Database | PostgreSQL | Video and active-like records |
| Schema changes | `golang-migrate` with versioned SQL files | Apply and roll back explicit database changes before the API starts; do not use GORM `AutoMigrate` |
| Local runtime | Docker Compose | Reproducible app and observability containers |
| Metrics | Prometheus Go client, Prometheus, Grafana | Expose `/metrics` for request, Go runtime, and database pool measurements |
| Traces | OpenTelemetry Go SDK, Gin and GORM instrumentation, OTLP, Collector, Tempo, Grafana | Follow HTTP requests and database operations |
| Load | k6 | Repeatable read and like workloads |

Image and library versions are pinned. Compose runs migrations after Postgres is healthy and starts the API after the migrations finish. GORM models match the SQL migration; the migration files own the schema.

## Repository structure

```text
youtube-likes-counter/
├── README.md                         # Series overview and links to each level
├── level-1/
│   ├── README.md                     # This level's flows, choices, and run guide
│   ├── schema.dbml                   # Diagram for dbdiagram.io
│   ├── compose.yaml                  # App, database, migrations, and observability
│   ├── frontend/                     # React video card and Like/Unlike UI
│   ├── backend/
│   │   ├── cmd/api/main.go           # Start the Gin server
│   │   └── go.mod                    # Pinned Go dependencies
│   ├── migrations/                   # Versioned .up.sql and .down.sql files
│   ├── observability/                # Prometheus, Collector, Tempo, Grafana config
│   └── load/                         # k6 scenario
└── level-2/                          # Future level, after a measured bottleneck
```

Each level keeps its own runnable setup and records what changed from the previous level. Level 1 keeps its small Go API in one file; we can split it once the code needs clearer boundaries.

## Run locally

Start Docker Desktop and make sure its Linux container engine is running. In PowerShell, go to this level's folder and start the stack:

```powershell
Set-Location 'C:\Personal\Projects\cimulink-repositories\cimulink-live\projects\youtube-likes-counter\level-1'
docker compose up --build -d
docker compose ps
```

The first start builds the frontend and API images and downloads the pinned service images. Compose waits for PostgreSQL, applies both SQL migrations, then starts the API and frontend. Open the demo at [http://localhost:5173](http://localhost:5173). Grafana is at [http://localhost:3000](http://localhost:3000) (`admin` / `admin`), and Prometheus is at [http://localhost:9090](http://localhost:9090). Grafana provisions the **Like Counter · Level 1** dashboard and a Tempo trace data source.

To run the included load scenario from the same folder:

```powershell
docker compose --profile load run --rm k6
docker compose --profile load run --rm -e RATE=50 -e VIDEO_MODE=spread k6
```

`RATE` defaults to 5 requests per second, `WRITE_PERCENT` to 10, `DURATION` to `2m`, and `VIDEO_MODE` to `hot`. Use `VIDEO_MODE=spread` to distribute requests across the five seeded videos. To inspect service logs, run `docker compose logs -f api` (replace `api` with another service name as needed).

Stop the stack with `docker compose down`. This keeps the named database and dashboard volumes. To reset all local data and start fresh, run `docker compose down --volumes`; this deletes the local demo data.

The API is also available at `http://localhost:8080`. Its health endpoint is `/healthz` and its Prometheus endpoint is `/metrics`. The seeded primary video ID is `11111111-1111-4111-8111-111111111111`.

## Baseline experiment

The migration seeds five videos. First run a light read-heavy k6 workload, then increase requests per second while keeping the read/like mix and machine limits recorded. Include a case where many viewers target the same video and a case where they target different videos. k6 uses a new demo viewer ID for each request so every simulated Like is a new write; the browser keeps one stable ID so its Like/Unlike button behaves normally.

Record: requested and achieved requests/sec, p50/p95/p99 response time, error rate, k6 dropped iterations, backend CPU/memory, database CPU/memory and connections, and container network traffic. Compare the count returned by the API with the number of rows in `video_likes` after each run. Save the workload settings, machine/container limits, dashboard screenshots, and observations so later levels can be compared fairly.

Prometheus supplies the app metrics dashboard; use Docker container stats for the initial database and network resource measurements.

This local run measures resource use and user-visible performance. Cloud cost is an estimate based on the capacity needed, not a measured bill.

## Level 1 boundary

No login, recommendation feed, cache, queue, replicas, or Kubernetes. We add a change only after a load run shows the problem it addresses.

## References

- [Gin middleware](https://gin-gonic.com/en/docs/middleware/)
- [GORM PostgreSQL driver](https://gorm.io/docs/connecting_to_the_database.html)
- [golang-migrate SQL migrations](https://github.com/golang-migrate/migrate)
- [Prometheus Go instrumentation](https://prometheus.io/docs/guides/go-application/)
- [OpenTelemetry Gin instrumentation](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin)
- [GORM OpenTelemetry plugin](https://github.com/go-gorm/opentelemetry)
- [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/)
- [Grafana Tempo local deployment](https://grafana.com/docs/tempo/latest/set-up-for-tracing/setup-tempo/deploy/locally/docker-compose/)
- [k6 request-rate workloads](https://grafana.com/docs/k6/latest/testing-guides/api-load-testing/)
