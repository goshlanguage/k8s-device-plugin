# Important to note, using golang:1.25 or other images can have unanticipated side effects in other architectures like arm64
#   If you change this and the build breaks, its because there are toolchain inconsistencies across images per achitecture
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build

WORKDIR /src

# Copy go.mod and go.sum first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire project
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o device-plugin ./cmd/k8s-device-plugin/main.go

FROM scratch
COPY --from=build /src/device-plugin /device-plugin
ENTRYPOINT ["/device-plugin"]
