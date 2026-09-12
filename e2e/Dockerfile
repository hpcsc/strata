FROM golang:1.26 AS builder
# go.mod can ask for a newer Go patch release than the image has
ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG BUILD_TAG=v0.1.0
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/hpcsc/strata/internal/version.releaseTag=${BUILD_TAG}" \
    -o /strata ./cmd/strata

# strata needs git 2.41 or later and strata sync 2.44; Debian trixie ships 2.47, bookworm only 2.39
FROM node:24-trixie-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends git python3 make g++ \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /e2e
COPY e2e/package.json e2e/package-lock.json ./
RUN npm ci
COPY e2e ./
COPY --from=builder /strata /usr/local/bin/strata
ARG BUILD_TAG=v0.1.0
ENV EXECUTABLE=/usr/local/bin/strata BUILD_TAG=${BUILD_TAG}
CMD ["npx", "vitest", "run"]
