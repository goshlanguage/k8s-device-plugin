FROM golang:1.25 as build

WORKDIR /src

# Copy go.mod and go.sum first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire project
COPY . .

# Set working directory to the binary entrypoint
WORKDIR /src/cmd/k8s-device-plugin

RUN go build -o /device-plugin .

FROM cgr.dev/chainguard/wolfi-base:latest
COPY --from=build /device-plugin /device-plugin
CMD ["ls"]
