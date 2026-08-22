# ---- build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /src

RUN apk add --no-cache git

# Cache module downloads separately from source changes
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Pin templ to the exact version this module was built against (see go.mod)
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1020
RUN templ generate
RUN CGO_ENABLED=0 go build -trimpath -o /out/app ./cmd/api

# ---- runtime stage ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H appuser
WORKDIR /app

# web/static is served relative to the working directory (Static("/static",
# "./web/static") plus the root-served /sw.js and /manifest.json - see
# routes.go) - migrations are compiled in via go:embed, so nothing else
# from the source tree needs to come along.
COPY --from=build /src/web/static ./web/static
COPY --from=build /out/app ./app
USER appuser

ENTRYPOINT ["./app"]
