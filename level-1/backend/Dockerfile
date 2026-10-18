FROM golang:1.25.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /likes-api ./cmd/api

FROM scratch
COPY --from=build /likes-api /likes-api
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/likes-api"]
