# Temporary notes: Docker, Go dependencies, frontend, and observability

## Previous question

> `level-1/observability`: on a high level, help me understand each backend file and its purpose here. Also, in backend and frontend there are some files I would like to understand: the purpose of `.dockerignore`; whether it is for pushing to a Docker registry; the backend `Dockerfile`; what `go mod download`, `COPY . .`, `CGO_ENABLED=0`, `GOOS=linux`, `FROM scratch`, `EXPOSE`, and `ENTRYPOINT` do; whether more APIs need more entrypoints; the two `require` groups in `go.mod`; why `go.sum` contains ClickHouse and Bytedance packages; why the frontend needs a separate Dockerfile and Nginx; why `package-lock.json` is needed and committed; and the purpose of `tsconfig.json`.

## Previous answer

First, one small fix I made while inspecting: the backend `.dockerignore` accidentally contained the demo video seed SQL. I cleaned it up and restored the seed as its own migration file. The `.dockerignore` now just lists `.git` and `*.exe`.

### How the local stack fits together

```text
Browser → Nginx serves the React page
        → Nginx forwards /api requests to the Go API
        → Go API uses GORM to query PostgreSQL

Prometheus scrapes API metrics → Grafana displays them
Go sends traces → OpenTelemetry Collector → Tempo → Grafana
k6 sends simulated requests → Go API
```

### Docker builds and local images

When you run `docker compose up --build`, Compose builds local images for the API and frontend. It also downloads the database, migration tool, and observability images from registries. Nothing is pushed to a registry. Pushing would require a separate command such as `docker push`. Docker’s `.dockerignore` controls which local files are sent to the image builder; it does not publish anything.

The backend Dockerfile has two stages:

1. **Build stage:** `FROM golang:1.25.4-alpine AS build` provides Go and Alpine Linux. `WORKDIR /src` sets the working folder inside that stage.
2. **Dependency layer:** `COPY go.mod go.sum ./` copies the Go dependency files first. `RUN go mod download` downloads dependency source into Go’s cache inside the build environment. It does not install a separate app or add dependencies to the final image. Keeping this before the source copy lets Docker reuse the downloaded-dependencies layer when only our Go code changes.
3. **Source and compile:** `COPY . .` copies the backend build folder’s contents into `/src`, excluding `.git` and `.exe` files because they are in `.dockerignore`. `RUN CGO_ENABLED=0 GOOS=linux go build -o /likes-api ./cmd/api` compiles the API for Linux and writes the executable to `/likes-api`. `CGO_ENABLED=0` avoids relying on C libraries, which helps the executable run in a very small final image.
4. **Run stage:** `FROM scratch` starts an empty image. `COPY --from=build` brings just the compiled executable into it; the Go compiler and source code stay in the build stage. Multi-stage builds work this way to keep build tools out of the final image.
5. `USER 65532:65532` runs the app as a non-root user. `EXPOSE 8080` documents the port the app listens on; Compose’s `ports` setting is what makes it reachable from your computer.
6. `ENTRYPOINT ["/likes-api"]` tells Docker which program to start when this container runs. If we add more API routes, they’ll be handled by this same Go server; each route does not need its own entry point. We’d use another entry point for a separate program or service.

The backend `.dockerignore` keeps Git history and Windows `.exe` files out of the build context. It helps the build stay smaller and avoids accidentally copying a Windows binary into a Linux image.

### Go dependency files

In `go.mod`, the first `require` group lists dependencies our API imports directly, such as Gin, GORM, and OpenTelemetry. The second group, marked `// indirect`, lists dependencies that those libraries use. Both groups are part of the build; “indirect” just means our source code doesn’t import them directly.

The names you asked about have concrete paths in the dependency tree:

- **ClickHouse** comes through the GORM OpenTelemetry plugin. Its tracing code imports drivers for several databases, including ClickHouse and MySQL, to support those database types. We use PostgreSQL, but that plugin dependency still brings those drivers into its dependency graph.
- **Bytedance Sonic** comes through Gin’s JSON support. It handles JSON serialization; it has nothing to do with generating videos.

I checked those paths with `go mod why`. The extra dependencies are a cost of the GORM tracing plugin, not features we chose for the app.

`go.sum` records checksums so Go can verify downloaded module files. It can contain more entries than the direct dependencies because it tracks indirect modules too. Both `go.mod` and `go.sum` belong in Git; Go’s documentation describes them as the dependency list and its integrity checks.

### Frontend files

The frontend gets its own Docker image because it has a different build and run path from the Go API:

- The frontend Dockerfile uses Node to build React and TypeScript into static HTML, JavaScript, and CSS files. Its second stage uses Nginx to serve those files. The Node build tools don’t need to be in the final image.
- `nginx.conf` tells Nginx how to serve the page and forward `/api/` requests to the Go service. React runs in the user’s browser; it doesn’t serve the production site files itself. Nginx also sends page routes back to `index.html`, which lets the single page app load.
- The frontend `.dockerignore` excludes `node_modules`, previous build output, and Git history from that image’s build context.
- `package.json` declares the frontend’s direct dependencies and scripts. `package-lock.json` records the resolved versions of the full dependency tree. We commit both. `npm ci` uses the lockfile for a repeatable build and errors if it conflicts with `package.json`; after intentionally changing dependencies, run `npm install` to update the lockfile.
- `tsconfig.json` tells TypeScript how to check the code: which browser features and JavaScript version to expect, how to resolve imports, and how to interpret React’s JSX syntax. `tsc -b` uses this configuration during the frontend build.

### Observability files

- `prometheus.yml` tells Prometheus to scrape `api:8080` every five seconds. `api` is the service name on Compose’s private network; Prometheus collects the numbers published at `/metrics`.
- `otel-collector.yaml` receives OpenTelemetry traces over HTTP, batches them, and forwards them to Tempo. This config carries traces; it does not set up a log pipeline.
- `tempo.yaml` configures Tempo to receive and store traces locally for this learning setup.
- `grafana/datasources.yaml` connects Grafana to Prometheus for metrics and Tempo for traces.
- `grafana/dashboards.yaml` tells Grafana where to find dashboard files.
- `grafana/level-1.json` defines the dashboard panels and their queries. It displays metrics; it doesn’t collect them.

So yes: we build and run the images locally. The build may download base images and libraries, but we do not upload our images anywhere.
